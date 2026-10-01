package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Scope keys apart from the browser's random submission tokens. The first
// successful creation owns a key permanently, using the existing unique column.
func apiCreationKey(r *http.Request) (string, error) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) == 0 {
		return randomKey(), nil
	}
	if len(values) != 1 || len(values[0]) < 1 || len(values[0]) > 128 {
		return "", errors.New("Idempotency-Key must appear once and contain 1 to 128 ASCII letters, digits, dots, underscores, or hyphens")
	}
	for _, c := range values[0] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return "", errors.New("Idempotency-Key may contain only ASCII letters, digits, dots, underscores, or hyphens")
		}
	}
	return "api-create:" + values[0], nil
}

func (a *App) apiCreatedAsset(w http.ResponseWriter, id int64, status int) {
	w.Header().Set("Location", fmt.Sprintf("/api/assets/%d", id))
	a.apiEditedAsset(w, id, status)
}

func (a *App) apiCreationReplay(w http.ResponseWriter, key string) (handled bool) {
	existing, err := a.store.BySubmission(key)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		apiFailure(w, err)
		return true
	}
	a.apiCreatedAsset(w, existing.ID, http.StatusOK)
	return true
}

func (a *App) apiCreateAsset(w http.ResponseWriter, r *http.Request) {
	defer cleanupForm(r)
	key, err := apiCreationKey(r)
	if err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	form, ok := a.apiMultipart(w, r)
	if !ok {
		return
	}
	for field, values := range form.Value {
		switch field {
		case "description", "location":
			if len(values) != 1 {
				apiError(w, 400, "invalid_request", "Submit one value for "+field)
				return
			}
		case "caption_overview", "caption_photos", "group_photos":
		default:
			apiError(w, 400, "invalid_request", "Unknown creation field: "+field)
			return
		}
	}
	for field := range form.File {
		if field != "overview" && field != "photos" {
			apiError(w, 400, "invalid_request", "Only overview and photos accept file uploads.")
			return
		}
	}
	description := ""
	location := ""
	if values := form.Value["description"]; len(values) == 1 {
		description = strings.TrimSpace(values[0])
	}
	if values := form.Value["location"]; len(values) == 1 {
		location = strings.TrimSpace(values[0])
	}
	asset := Asset{Description: description, Location: location}
	if err := validate(asset); err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	if len(form.File["overview"]) != 1 {
		apiError(w, 400, "invalid_request", "Attach exactly one overview photo.")
		return
	}
	// A replay returns the current record and never changes its original intake.
	// Clients must use a fresh key for a different logical creation request.
	if a.apiCreationReplay(w, key) {
		return
	}
	for _, files := range form.File {
		for _, file := range files {
			if file.Size > maxPhotoBytes {
				apiError(w, 413, "request_too_large", "Each photo must be 12 MiB or smaller.")
				return
			}
		}
	}
	photos, err := saveUploads(a.dir, form, true)
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
	asset.Photos = groupedPhotos(photos, photos[0].Path, "", nil)
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.apiCreationReplay(w, key) {
		return
	}
	id, err := a.store.Create(asset, key)
	if err != nil {
		apiFailure(w, err)
		return
	}
	committed = true
	a.apiCreatedAsset(w, id, http.StatusCreated)
}
