package catalog

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"syscat/internal/buildinfo"
)

//go:embed openapi.json
var openAPISpec []byte

type apiPhoto struct {
	ID           string `json:"id"`
	APIURL       string `json:"api_url"`
	Name         string `json:"original_name"`
	Caption      string `json:"caption"`
	Group        string `json:"group"`
	Role         string `json:"role"`
	OriginalURL  string `json:"original_url"`
	ThumbnailURL string `json:"thumbnail_url"`
}

type apiAsset struct {
	ID             int64           `json:"id"`
	Label          string          `json:"label"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Location       string          `json:"location"`
	Photos         []apiPhoto      `json:"photos"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	Revision       int             `json:"revision"`
	Archived       bool            `json:"archived"`
	URL            string          `json:"url"`
	APIURL         string          `json:"api_url"`
	PhotoUploadURL string          `json:"photo_upload_url"`
	Intake         json.RawMessage `json:"original_intake,omitempty"`
}

type apiAssetList struct {
	Assets []apiAsset `json:"assets"`
	apiListPage
}

type apiListPage struct {
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Query    string `json:"query"`
	Field    string `json:"field,omitempty"`
	Archived bool   `json:"archived"`
	Previous string `json:"previous,omitempty"`
	Next     string `json:"next,omitempty"`
}

type apiAssetSummary struct {
	ID         int64  `json:"id"`
	Label      string `json:"label"`
	Title      string `json:"title"`
	Revision   int    `json:"revision"`
	PhotoCount int    `json:"photo_count"`
	Archived   bool   `json:"archived"`
	URL        string `json:"url"`
	APIURL     string `json:"api_url"`
}

type apiAssetSummaryList struct {
	Assets []apiAssetSummary `json:"assets"`
	View   string            `json:"view"`
	apiListPage
}

type apiErrorResponse struct {
	Error apiErrorDetail `json:"error"`
}

type apiErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func asAPIAsset(c Asset) apiAsset {
	photos := make([]apiPhoto, len(c.Photos))
	for i, photo := range c.Photos {
		role := "detail"
		if i == 0 {
			role = "overview"
		}
		photos[i] = apiPhoto{ID: photo.ID(), APIURL: fmt.Sprintf("/api/assets/%d/photos/%s", c.ID, photo.ID()), Name: photo.Name, Caption: photo.Caption, Group: photo.Group, Role: role, OriginalURL: "/" + photo.Path, ThumbnailURL: "/" + photo.Thumbnail}
	}
	return apiAsset{
		ID: c.ID, Label: c.Label(), Title: strings.TrimSpace(c.Title()),
		Description: c.Description, Location: c.Location, Photos: photos,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Revision: c.Revision,
		Archived: c.Archived, URL: fmt.Sprintf("/assets/%d", c.ID),
		APIURL: fmt.Sprintf("/api/assets/%d", c.ID), Intake: c.Intake,
		PhotoUploadURL: fmt.Sprintf("/api/assets/%d/photos", c.ID),
	}
}

func apiJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("API response failed: %v", err)
		status = http.StatusInternalServerError
		data = []byte(`{"error":{"code":"internal_error","message":"Unable to complete the request."}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}

func apiError(w http.ResponseWriter, status int, code, message string) {
	apiJSON(w, status, apiErrorResponse{apiErrorDetail{Code: code, Message: message}})
}

func apiFailure(w http.ResponseWriter, err error) {
	log.Printf("API request failed: %v", err)
	apiError(w, http.StatusInternalServerError, "internal_error", "Unable to complete the request.")
}

func apiInteger(query url.Values, key string, fallback, maximum int) (int, error) {
	if !query.Has(key) {
		return fallback, nil
	}
	n, err := strconv.Atoi(query.Get(key))
	if err != nil || n < 1 || n > maximum {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", key, maximum)
	}
	return n, nil
}

func (a *App) apiList(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", "Malformed query string.")
		return
	}
	for key, values := range query {
		switch key {
		case "q", "page", "page_size", "archived", "view", "field":
		default:
			apiError(w, http.StatusBadRequest, "invalid_request", "Unknown query parameter: "+key)
			return
		}
		if len(values) != 1 {
			apiError(w, http.StatusBadRequest, "invalid_request", "Query parameters must appear only once: "+key)
			return
		}
	}
	page, err := apiInteger(query, "page", 1, 1_000_000)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	size, err := apiInteger(query, "page_size", 50, 100)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	archived := false
	switch query.Get("archived") {
	case "", "0", "false":
	case "1", "true":
		archived = true
	default:
		apiError(w, http.StatusBadRequest, "invalid_request", "archived must be 0, 1, false, or true")
		return
	}
	field := "all"
	if query.Has("field") {
		field = query.Get("field")
	}
	switch field {
	case "all", "title", "description", "location", "caption", "id":
	default:
		apiError(w, 400, "invalid_request", "field must be all, title, description, location, caption, or id")
		return
	}
	search := strings.TrimSpace(query.Get("q"))
	view := "full"
	if query.Has("view") {
		view = query.Get("view")
	}
	if view != "full" && view != "summary" {
		apiError(w, 400, "invalid_request", "view must be full or summary")
		return
	}
	var entries []Asset
	var summaries []AssetSummary
	var total int
	if view == "summary" {
		summaries, total, err = a.store.listSummariesWithField(search, archived, size, (page-1)*size, field)
	} else {
		entries, total, err = a.store.listWithField(search, archived, size, (page-1)*size, field)
	}
	if err != nil {
		apiFailure(w, err)
		return
	}
	result := apiListPage{Total: total, Page: page, PageSize: size, Query: search, Archived: archived}
	if query.Has("field") {
		result.Field = field
	}
	link := func(number int) string {
		values := url.Values{"page": {strconv.Itoa(number)}, "page_size": {strconv.Itoa(size)}}
		if search != "" {
			values.Set("q", search)
		}
		if archived {
			values.Set("archived", "1")
		}
		if query.Has("field") {
			values.Set("field", field)
		}
		if query.Has("view") {
			values.Set("view", view)
		}
		return "/api/assets?" + values.Encode()
	}
	if page > 1 {
		result.Previous = link(page - 1)
	}
	if page*size < total {
		result.Next = link(page + 1)
	}
	if view == "summary" {
		assets := make([]apiAssetSummary, len(summaries))
		for i, summary := range summaries {
			assets[i] = apiAssetSummary{ID: summary.ID, Label: fmt.Sprintf("%05d", summary.ID), Title: summary.Title, Revision: summary.Revision, PhotoCount: summary.PhotoCount, Archived: summary.Archived, URL: fmt.Sprintf("/assets/%d", summary.ID), APIURL: fmt.Sprintf("/api/assets/%d", summary.ID)}
		}
		apiJSON(w, 200, apiAssetSummaryList{Assets: assets, View: "summary", apiListPage: result})
		return
	}
	assets := make([]apiAsset, len(entries))
	for i, entry := range entries {
		entry.Intake = nil
		assets[i] = asAPIAsset(entry)
	}
	apiJSON(w, 200, apiAssetList{Assets: assets, apiListPage: result})
}

func (a *App) apiDetail(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := strconv.ParseInt(rawID, 10, 64)
	digits := rawID != ""
	for _, char := range rawID {
		if char < '0' || char > '9' {
			digits = false
		}
	}
	if err != nil || id < 1 || !digits {
		apiError(w, http.StatusBadRequest, "invalid_id", "Inventory ID must be a positive integer.")
		return
	}
	entry, err := a.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "not_found", "Entry not found.")
		return
	}
	if err != nil {
		apiFailure(w, err)
		return
	}
	apiJSON(w, http.StatusOK, asAPIAsset(entry))
}

func (a *App) apiIndex(w http.ResponseWriter, r *http.Request) {
	apiJSON(w, http.StatusOK, map[string]any{
		"name":                 "Syscat",
		"build":                buildinfo.String(),
		"read_only":            false,
		"write_authentication": "trusted_network",
		"links": map[string]string{
			"self":      "/api",
			"assets":    "/api/assets",
			"summaries": "/api/assets?view=summary",
			"openapi":   "/api/openapi.json",
		},
	})
}

func (a *App) apiSpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(openAPISpec)
}

func (a *App) apiMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	allow := "GET, HEAD"
	if r.Pattern == "/api/assets" {
		allow = "GET, HEAD, POST"
	}
	if r.Pattern == "/api/assets/{id}" {
		allow = "GET, HEAD, PATCH"
	}
	if r.Pattern == "/api/assets/{id}/photos" {
		allow = "POST"
	}
	if r.Pattern == "/api/assets/{id}/photos/{photo_id}" {
		allow = "PATCH"
	}
	w.Header().Set("Allow", allow)
	apiError(w, http.StatusMethodNotAllowed, "method_not_allowed", "This method is not supported by this API endpoint.")
}

func (a *App) apiNotFound(w http.ResponseWriter, r *http.Request) {
	apiError(w, http.StatusNotFound, "not_found", "API endpoint not found.")
}
