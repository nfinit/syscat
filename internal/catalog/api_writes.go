package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

const maxAPIWriteBytes = 128 << 10

// Every write route passes through this policy, before parsing or mutation.
// Token/session authorization can replace it and issue JSON 401/403 responses
// without changing handlers or request formats. Trusted-network mode is explicit.
func trustedAPIWrite(http.ResponseWriter, *http.Request) bool { return true }

func (a *App) apiWrite(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.apiWritePolicy(w, r) {
			return
		}
		next(w, r)
	}
}

// Parse an object strictly: reject unknown/duplicate keys, null values, multiple
// objects, and unsupported types instead of silently ignoring an agent's edits.
func apiPatchObject(w http.ResponseWriter, r *http.Request, fields ...string) (map[string]json.RawMessage, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use Content-Type: application/json.")
		return nil, false
	}
	if r.URL.RawQuery != "" {
		apiError(w, http.StatusBadRequest, "invalid_request", "JSON write endpoints do not accept query parameters.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIWriteBytes)
	decoder := json.NewDecoder(r.Body)
	allowed := map[string]bool{"revision": true}
	for _, field := range fields {
		allowed[field] = true
	}
	values := make(map[string]json.RawMessage)
	parse := func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if token != json.Delim('{') {
			return errors.New("submit a JSON object")
		}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if !allowed[key] {
				return fmt.Errorf("unsupported field %q", key)
			}
			if _, exists := values[key]; exists {
				return fmt.Errorf("duplicate field %q", key)
			}
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				return err
			}
			if string(value) == "null" {
				return fmt.Errorf("%s cannot be null; use an empty string to clear optional text", key)
			}
			values[key] = value
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
		var extra json.RawMessage
		if err := decoder.Decode(&extra); err != io.EOF {
			if err != nil {
				return err
			}
			return errors.New("submit exactly one JSON object")
		}
		if len(values) < 2 {
			return errors.New("include revision and at least one editable field")
		}
		return nil
	}
	if err := parse(); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			apiError(w, http.StatusRequestEntityTooLarge, "request_too_large", "JSON writes are limited to 128 KiB.")
		} else {
			apiError(w, http.StatusBadRequest, "invalid_request", err.Error())
		}
		return nil, false
	}
	return values, true
}

func apiPatchRevision(values map[string]json.RawMessage) (int, error) {
	var revision int
	raw, exists := values["revision"]
	if !exists || json.Unmarshal(raw, &revision) != nil || revision < 1 {
		return 0, errors.New("revision must be the positive integer from the latest asset read")
	}
	return revision, nil
}

func apiPatchText(values map[string]json.RawMessage, field string, target *string) error {
	raw, exists := values[field]
	if !exists {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s must be a string", field)
	}
	*target = strings.TrimSpace(value)
	return nil
}

func (a *App) apiWriteAsset(w http.ResponseWriter, r *http.Request, revision int) (Asset, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	valid := raw != ""
	for _, char := range raw {
		if char < '0' || char > '9' {
			valid = false
		}
	}
	if err != nil || id < 1 || !valid {
		apiError(w, 400, "invalid_id", "Record ID must be a positive integer.")
		return Asset{}, false
	}
	c, err := a.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, 404, "not_found", "Entry not found.")
		return Asset{}, false
	}
	if err != nil {
		apiFailure(w, err)
		return Asset{}, false
	}
	if c.Revision != revision {
		apiError(w, 409, "conflict", "Entry modified elsewhere. Read the latest asset and retry with its revision.")
		return Asset{}, false
	}
	if c.Archived {
		apiError(w, 409, "conflict", "Archived entries cannot be edited. Restore the entry through the web interface first.")
		return Asset{}, false
	}
	return c, true
}

func (a *App) apiCommitEdits(w http.ResponseWriter, c Asset) bool {
	if err := a.saveAssetEdits(c, nil); err != nil {
		switch {
		case errors.Is(err, ErrConflict):
			apiError(w, 409, "conflict", "Entry modified elsewhere. Read the latest asset and retry with its revision.")
		case errors.Is(err, errInvalidEdit):
			apiError(w, 400, "invalid_request", err.Error())
		default:
			apiFailure(w, err)
		}
		return false
	}
	return true
}

func (a *App) apiSaveEdits(w http.ResponseWriter, c Asset) {
	if a.apiCommitEdits(w, c) {
		a.apiEditedAsset(w, c.ID, http.StatusOK)
	}
}

func (a *App) apiEditedAsset(w http.ResponseWriter, id int64, status int) {
	saved, err := a.store.Get(id)
	if err != nil {
		apiFailure(w, err)
		return
	}
	apiJSON(w, status, asAPIAsset(saved))
}

func (a *App) apiPatchAsset(w http.ResponseWriter, r *http.Request) {
	values, ok := apiPatchObject(w, r, "description", "location")
	if !ok {
		return
	}
	revision, err := apiPatchRevision(values)
	if err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	c, ok := a.apiWriteAsset(w, r, revision)
	if !ok {
		return
	}
	if err := apiPatchText(values, "description", &c.Description); err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	if err := apiPatchText(values, "location", &c.Location); err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	a.apiSaveEdits(w, c)
}

func (a *App) apiPatchPhoto(w http.ResponseWriter, r *http.Request) {
	values, ok := apiPatchObject(w, r, "caption", "group")
	if !ok {
		return
	}
	revision, err := apiPatchRevision(values)
	if err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	c, ok := a.apiWriteAsset(w, r, revision)
	if !ok {
		return
	}
	var groupOrder []string
	for _, section := range c.PhotoSections() {
		if section.Name != "" {
			groupOrder = append(groupOrder, section.Name)
		}
	}
	index := -1
	for i, photo := range c.Photos {
		if photo.ID() == r.PathValue("photo_id") {
			index = i
			break
		}
	}
	if index < 0 {
		apiError(w, 404, "not_found", "Photo not found on this entry.")
		return
	}
	if err := apiPatchText(values, "caption", &c.Photos[index].Caption); err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	if err := apiPatchText(values, "group", &c.Photos[index].Group); err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	// Validate before regrouping, which intentionally clears the overview's group.
	if index == 0 && c.Photos[index].Group != "" {
		apiError(w, 400, "invalid_request", "The overview photo cannot belong to a group.")
		return
	}
	c.Photos = groupedPhotos(c.Photos, c.Photos[0].Path, "", groupOrder)
	a.apiSaveEdits(w, c)
}
