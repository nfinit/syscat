package catalog

import (
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
)

// Accept file bytes through the same privacy/thumbnail pipeline as form uploads.
// Expensive image processing occurs outside writeMu; recheck the revision before
// committing, and remove all new files if any part of the request fails.
func (a *App) apiUploadPhotos(w http.ResponseWriter, r *http.Request) {
	defer cleanupForm(r)
	form, ok := a.apiMultipart(w, r)
	if !ok {
		return
	}
	var err error
	for key := range form.Value {
		switch key {
		case "revision", "caption_photos", "group_photos":
		default:
			apiError(w, 400, "invalid_request", "Unknown upload field: "+key)
			return
		}
	}
	for key := range form.File {
		if key != "photos" {
			apiError(w, 400, "invalid_request", "Only photos accepts file uploads.")
			return
		}
	}
	values := form.Value["revision"]
	revision := 0
	if len(values) == 1 {
		raw := values[0]
		revision, err = strconv.Atoi(raw)
		if strings.Trim(raw, "0123456789") != "" {
			revision = 0
		}
	}
	if len(values) != 1 || err != nil || revision < 1 {
		apiError(w, 400, "invalid_request", "Submit one positive integer revision from the latest asset read.")
		return
	}
	if len(form.File["photos"]) == 0 {
		apiError(w, 400, "invalid_request", "Attach at least one file in photos.")
		return
	}
	if _, ok := a.apiWriteAsset(w, r, revision); !ok {
		return
	}
	for _, file := range form.File["photos"] {
		if file.Size > maxPhotoBytes {
			apiError(w, 413, "request_too_large", "Each photo must be 12 MiB or smaller.")
			return
		}
	}
	photos, err := saveUploads(a.dir, form, false)
	if err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			removePhotos(a.dir, photos)
		}
	}()
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	asset, ok := a.apiWriteAsset(w, r, revision)
	if !ok {
		return
	}
	var groupOrder []string
	for _, section := range asset.PhotoSections() {
		if section.Name != "" {
			groupOrder = append(groupOrder, section.Name)
		}
	}
	overview := ""
	if len(asset.Photos) > 0 {
		overview = asset.Photos[0].Path
	} else {
		if photos[0].Group != "" {
			apiError(w, 400, "invalid_request", "The first photo becomes the overview and cannot belong to a group.")
			return
		}
		overview = photos[0].Path
	}
	// For an older record with no photos, its first attachment becomes overview.
	asset.Photos = groupedPhotos(append(asset.Photos, photos...), overview, overview, groupOrder)
	if !a.apiCommitEdits(w, asset) {
		return
	}
	committed = true
	w.Header().Set("Location", fmt.Sprintf("/api/assets/%d", asset.ID))
	a.apiEditedAsset(w, asset.ID, http.StatusCreated)
}

// Callers defer cleanupForm before parsing, including malformed/oversized requests.
func (a *App) apiMultipart(w http.ResponseWriter, r *http.Request) (*multipart.Form, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		apiError(w, 415, "unsupported_media_type", "Use Content-Type: multipart/form-data with a boundary.")
		return nil, false
	}
	if r.URL.RawQuery != "" {
		apiError(w, 400, "invalid_request", "Multipart writes do not accept query parameters.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.maxUploadMiB<<20)
	err = r.ParseMultipartForm(2 << 20)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			apiError(w, 413, "request_too_large", fmt.Sprintf("Maximum total upload request size is %d MiB.", a.maxUploadMiB))
		} else {
			apiError(w, 400, "invalid_request", "Unable to read multipart upload.")
		}
		return nil, false
	}
	return r.MultipartForm, true
}
