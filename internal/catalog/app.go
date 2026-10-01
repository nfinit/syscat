package catalog

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"syscat/internal/buildinfo"
)

//go:embed templates/*.html static/*
var assets embed.FS

type session struct {
	ID, CSRF, Location string
	Expires            time.Time
}

const DefaultMaxUploadMiB int64 = 256

type App struct {
	maxUploadMiB   int64
	store          *Store
	dir            string
	templates      *template.Template
	sessions       map[string]session
	sessionMu      sync.Mutex
	writeMu        sync.Mutex
	handler        http.Handler
	apiWritePolicy func(http.ResponseWriter, *http.Request) bool
}

type page struct {
	MaxUploadMiB                                        int64
	Build                                               string
	Page, Title, Error, Notice, CSRF, Submission, Query string
	Asset                                               Asset
	Original                                            *Asset
	Assets                                              []Asset
	Locations                                           []string
	Count                                               int
	Archived                                            bool
	Saved                                               *Asset
	DeletedPhotos                                       map[string]bool
	Previous, Next                                      string
	NumberTarget                                        int64
	NumberOther                                         *Asset
	NumberPreview                                       bool
}

func New(dir string) (*App, error) {
	return NewWithUploadLimit(dir, DefaultMaxUploadMiB)
}

// NewWithUploadLimit sets the total request limit, including multipart overhead.
func NewWithUploadLimit(dir string, maxUploadMiB int64) (*App, error) {
	if maxUploadMiB < 1 || maxUploadMiB > (1<<63-1)>>20 {
		return nil, errors.New("max-upload-mib must be a positive whole number within the supported byte range")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{dir, filepath.Join(dir, "photos"), filepath.Join(dir, "thumbnails")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
	}
	store, err := Open(dir)
	if err != nil {
		return nil, err
	}
	t, err := template.New("").Funcs(template.FuncMap{"inc": func(n int) int { return n + 1 }}).ParseFS(assets, "templates/*.html")
	if err != nil {
		store.Close()
		return nil, err
	}
	a := &App{maxUploadMiB: maxUploadMiB, store: store, dir: dir, templates: t, sessions: make(map[string]session)}
	a.apiWritePolicy = trustedAPIWrite
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/assets/new", http.StatusSeeOther) })
	mux.HandleFunc("GET /assets/new", a.newAsset)
	mux.HandleFunc("GET /assets", a.list)
	mux.HandleFunc("POST /assets", a.create)
	mux.HandleFunc("GET /assets/{id}", a.detail)
	mux.HandleFunc("GET /assets/{id}/edit", a.edit)
	mux.HandleFunc("GET /assets/{id}/catalog-number", a.numberEditor)
	mux.HandleFunc("POST /assets/{id}/catalog-number", a.numberChange)
	mux.HandleFunc("POST /assets/{id}", a.update)
	mux.HandleFunc("POST /assets/{id}/archive", a.archive)
	mux.HandleFunc("GET /export/{format}", a.export)
	mux.HandleFunc("GET /api/assets", a.apiList)
	mux.HandleFunc("POST /api/assets", a.apiWrite(a.apiCreateAsset))
	mux.HandleFunc("/api/assets", a.apiMethodNotAllowed)
	mux.HandleFunc("GET /api/assets/{id}", a.apiDetail)
	mux.HandleFunc("PATCH /api/assets/{id}", a.apiWrite(a.apiPatchAsset))
	mux.HandleFunc("/api/assets/{id}", a.apiMethodNotAllowed)
	mux.HandleFunc("POST /api/assets/{id}/catalog-number", a.apiWrite(a.apiNumberChange))
	mux.HandleFunc("/api/assets/{id}/catalog-number", a.apiMethodNotAllowed)
	mux.HandleFunc("POST /api/assets/{id}/photos", a.apiWrite(a.apiUploadPhotos))
	mux.HandleFunc("/api/assets/{id}/photos", a.apiMethodNotAllowed)
	mux.HandleFunc("PATCH /api/assets/{id}/photos/{photo_id}", a.apiWrite(a.apiPatchPhoto))
	mux.HandleFunc("/api/assets/{id}/photos/{photo_id}", a.apiMethodNotAllowed)
	mux.HandleFunc("GET /api/openapi.json", a.apiSpec)
	mux.HandleFunc("/api/openapi.json", a.apiMethodNotAllowed)
	mux.HandleFunc("/api/", a.apiNotFound)
	mux.HandleFunc("GET /api", a.apiIndex)
	mux.HandleFunc("/api", a.apiMethodNotAllowed)
	mux.HandleFunc("GET /api/{$}", a.apiIndex)
	mux.HandleFunc("/api/{$}", a.apiMethodNotAllowed)
	mux.HandleFunc("GET /photos/{name}", a.photo)
	mux.HandleFunc("GET /thumbnails/{name}", a.photo)
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	a.handler = mux
	return a, nil
}

func (a *App) Close() error { return a.store.Close() }

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Link", `</api/openapi.json>; rel="service-desc"; type="application/json"`)
	a.handler.ServeHTTP(w, r)
}

func (a *App) getSession(w http.ResponseWriter, r *http.Request) session {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	now := time.Now()
	if cookie, err := r.Cookie("syscat_session"); err == nil {
		if s, ok := a.sessions[cookie.Value]; ok && now.Before(s.Expires) {
			s.Expires = now.Add(24 * time.Hour)
			a.sessions[s.ID] = s
			return s
		}
	}
	for key, s := range a.sessions {
		if now.After(s.Expires) {
			delete(a.sessions, key)
		}
	}
	if len(a.sessions) >= 4096 {
		// Evict the least recently used session rather than grow without a bound.
		var oldest string
		var expiry time.Time
		for key, s := range a.sessions {
			if oldest == "" || s.Expires.Before(expiry) {
				oldest, expiry = key, s.Expires
			}
		}
		delete(a.sessions, oldest)
	}
	s := session{ID: randomKey(), CSRF: randomKey(), Expires: now.Add(24 * time.Hour)}
	a.sessions[s.ID] = s
	http.SetCookie(w, &http.Cookie{Name: "syscat_session", Value: s.ID, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
	return s
}

func (a *App) remember(s session, c Asset) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	s.Location = c.Location
	a.sessions[s.ID] = s
}

func (a *App) render(w http.ResponseWriter, status int, p page) {
	p.Build = buildinfo.String()
	p.MaxUploadMiB = a.maxUploadMiB
	var b bytes.Buffer
	if err := a.templates.ExecuteTemplate(&b, "base", p); err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}

func (a *App) fail(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	http.Error(w, "Unable to complete the request. Check the server log for details.", http.StatusInternalServerError)
}

func (a *App) form(w http.ResponseWriter, status int, p page) {
	var err error
	p.Locations, err = a.store.Suggestions("location")
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, status, p)
}

func (a *App) newAsset(w http.ResponseWriter, r *http.Request) {
	s := a.getSession(w, r)
	p := page{Page: "form", Title: "New entry", CSRF: s.CSRF, Submission: randomKey(), Asset: Asset{Location: s.Location}}
	if id, err := strconv.ParseInt(r.URL.Query().Get("saved"), 10, 64); err == nil {
		if c, err := a.store.Get(id); err == nil && !c.Archived {
			p.Saved = &c
		}
	}
	if r.URL.Query().Get("undone") == "1" {
		p.Notice = "Entry archived. Restore it from the Archive page."
	}
	a.form(w, http.StatusOK, p)
}

func (a *App) parsePost(w http.ResponseWriter, r *http.Request, s session) error {
	r.Body = http.MaxBytesReader(w, r.Body, a.maxUploadMiB<<20)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(2 << 20)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		return fmt.Errorf("unable to read upload; maximum total request size is %d MiB", a.maxUploadMiB)
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(s.CSRF)) != 1 {
		return errors.New("form expired; open a new form in another tab and copy the entered values before saving")
	}
	return nil
}

func submitted(r *http.Request) (Asset, error) {
	c := Asset{Location: strings.TrimSpace(r.PostForm.Get("location"))}
	c.Revision, _ = strconv.Atoi(r.PostForm.Get("revision"))
	err := descriptionForm(r.PostForm, &c)
	return c, err
}

func validate(c Asset) error {
	if c.ShortDescription == "" && c.Details == "" {
		c.setLegacyDescription(c.Description)
	}
	if strings.TrimSpace(c.ShortDescription) == "" {
		return errors.New("a short description is required")
	}
	if strings.ContainsAny(c.ShortDescription, "\r\n") {
		return errors.New("short description must be a single line")
	}
	if len(c.ShortDescription) > 20000 || len(c.Details) > 20000 || len(c.Location) > 500 {
		return errors.New("field limits: short description and details 20,000 UTF-8 bytes each; location 500")
	}
	return nil
}

func cleanupForm(r *http.Request) {
	if r.MultipartForm != nil {
		_ = r.MultipartForm.RemoveAll()
	}
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	s := a.getSession(w, r)
	err := a.parsePost(w, r, s)
	defer cleanupForm(r)
	c, descriptionErr := submitted(r)
	if err == nil {
		err = descriptionErr
	}
	p := page{Page: "form", Title: "New entry", Asset: c, CSRF: s.CSRF, Submission: r.PostForm.Get("submission")}
	if err != nil {
		p.Error = err.Error()
		a.form(w, http.StatusBadRequest, p)
		return
	}
	if len(p.Submission) != 48 {
		p.Submission = randomKey()
		p.Error = "Entry token missing. Submit the form again."
		a.form(w, http.StatusBadRequest, p)
		return
	}
	if err := validate(c); err != nil {
		p.Error = err.Error()
		a.form(w, http.StatusBadRequest, p)
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if existing, err := a.store.BySubmission(p.Submission); err == nil {
		http.Redirect(w, r, fmt.Sprintf("/assets/%d", existing.ID), http.StatusSeeOther)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		a.fail(w, err)
		return
	}
	photos, err := saveUploads(a.dir, r.MultipartForm, true)
	if err != nil {
		p.Error = err.Error() + ". Entered text retained. Reselect photo files before submitting."
		a.form(w, http.StatusBadRequest, p)
		return
	}
	overview, err := selectedOverview(r.PostForm, nil, photos)
	if err != nil {
		removePhotos(a.dir, photos)
		p.Error = err.Error() + ". Reselect photo files before submitting."
		a.form(w, http.StatusBadRequest, p)
		return
	}
	c.Photos = groupedPhotos(photos, overview, "", nil)
	id, err := a.store.Create(c, p.Submission)
	if err != nil {
		removePhotos(a.dir, photos)
		a.fail(w, err)
		return
	}
	a.remember(s, c)
	http.Redirect(w, r, fmt.Sprintf("/assets/new?saved=%d", id), http.StatusSeeOther)
}

func (a *App) getAsset(w http.ResponseWriter, r *http.Request) (Asset, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return Asset{}, false
	}
	c, err := a.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return Asset{}, false
	}
	if err != nil {
		a.fail(w, err)
		return Asset{}, false
	}
	return c, true
}

func (a *App) detail(w http.ResponseWriter, r *http.Request) {
	c, ok := a.getAsset(w, r)
	if !ok {
		return
	}
	s := a.getSession(w, r)
	p := page{Page: "detail", Title: c.Label(), Asset: c, CSRF: s.CSRF}
	var original Asset
	if json.Unmarshal(c.Intake, &original) == nil {
		p.Original = &original
	}
	if r.URL.Query().Get("updated") == "1" {
		p.Notice = "Changes saved."
	}
	if r.URL.Query().Get("restored") == "1" {
		p.Notice = "Entry restored to Assets."
	}
	if c.Archived {
		a.render(w, http.StatusOK, p)
	} else {
		a.form(w, http.StatusOK, p)
	}
}

func (a *App) edit(w http.ResponseWriter, r *http.Request) {
	c, ok := a.getAsset(w, r)
	if !ok {
		return
	}
	if c.Archived {
		http.Redirect(w, r, fmt.Sprintf("/assets/%d", c.ID), http.StatusSeeOther)
		return
	}
	s := a.getSession(w, r)
	a.form(w, http.StatusOK, page{Page: "form", Title: "Edit entry " + c.Label(), Asset: c, CSRF: s.CSRF})
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	current, ok := a.getAsset(w, r)
	if !ok {
		return
	}
	s := a.getSession(w, r)
	err := a.parsePost(w, r, s)
	defer cleanupForm(r)
	c, descriptionErr := submitted(r)
	if err == nil {
		err = descriptionErr
	}
	if _, splitForm := r.PostForm["short_description"]; splitForm {
		// Browsers normalize textarea line endings. Preserve the stored notes
		// when their content is unchanged, including leading blank lines.
		if normalizedFormText(c.Details) == normalizedFormText(current.Details) {
			c.Details = current.Details
		}
		if c.ShortDescription == current.ShortDescription && c.Details == current.Details {
			c.Description = current.Description
		} else {
			c.projectDescription()
		}
	}
	c.CatalogNumber = current.CatalogNumber
	c.ID, c.Photos = current.ID, append([]Photo(nil), current.Photos...)
	if err == nil {
		err = submittedCaptions(r.PostForm, c.Photos)
	}
	if err == nil {
		err = submittedGroups(r.PostForm, c.Photos)
	}
	if err == nil {
		err = submittedPhotoOrder(r.PostForm, c.Photos)
	}
	var groupOrder []string
	if err == nil {
		groupOrder, err = submittedGroupOrder(r.PostForm)
	}
	deleted, deleteErr := submittedPhotoDeletions(r.PostForm, current.Photos)
	if err == nil {
		err = deleteErr
	}
	p := page{Page: "form", Title: "Edit entry " + c.Label(), Asset: c, CSRF: s.CSRF, DeletedPhotos: deleted}
	if err != nil {
		p.Error = err.Error()
		a.form(w, http.StatusBadRequest, p)
		return
	}
	if err := validate(c); err != nil {
		p.Error = err.Error()
		a.form(w, http.StatusBadRequest, p)
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	latest, err := a.store.Get(c.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	if latest.Revision != c.Revision || latest.Archived {
		p.Error = ErrConflict.Error() + ". Entered text retained below. Copy it before reopening the record."
		a.form(w, http.StatusConflict, p)
		return
	}
	photos, err := saveUploads(a.dir, r.MultipartForm, len(latest.Photos) == 0)
	if err != nil {
		p.Error = err.Error() + ". Entered text retained. Reselect photo files before submitting."
		a.form(w, http.StatusBadRequest, p)
		return
	}
	overview, err := selectedOverview(r.PostForm, current.Photos, photos)
	if err != nil {
		removePhotos(a.dir, photos)
		p.Error = err.Error() + ". Reselect photo files before submitting."
		a.form(w, http.StatusBadRequest, p)
		return
	}
	remaining := make([]Photo, 0, len(c.Photos))
	for _, photo := range c.Photos {
		if !deleted[photo.Path] {
			remaining = append(remaining, photo)
		}
	}
	if len(remaining)+len(photos) == 0 {
		p.Error = "Keep at least one photo or upload a replacement."
		a.form(w, http.StatusBadRequest, p)
		return
	}
	if deleted[overview] {
		if len(remaining) > 0 {
			overview = remaining[0].Path
		} else {
			overview = photos[0].Path
		}
	}
	formerOverview := ""
	if len(current.Photos) > 0 {
		formerOverview = current.Photos[0].Path
	}
	c.Photos = groupedPhotos(append(remaining, photos...), overview, formerOverview, groupOrder)
	if err := a.saveAssetEdits(c, deleted); err != nil {
		removePhotos(a.dir, photos)
		if errors.Is(err, ErrConflict) {
			p.Error = err.Error()
			a.form(w, http.StatusConflict, p)
		} else if errors.Is(err, errInvalidEdit) {
			p.Error = err.Error()
			a.form(w, http.StatusBadRequest, p)
		} else {
			a.fail(w, err)
		}
		return
	}
	removeDeletedPhotos(a.dir, current, deleted)
	http.Redirect(w, r, fmt.Sprintf("/assets/%d?updated=1", c.ID), http.StatusSeeOther)
}

func (a *App) archive(w http.ResponseWriter, r *http.Request) {
	c, ok := a.getAsset(w, r)
	if !ok {
		return
	}
	s := a.getSession(w, r)
	if err := a.parsePost(w, r, s); err != nil {
		cleanupForm(r)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer cleanupForm(r)
	revision, _ := strconv.Atoi(r.PostForm.Get("revision"))
	archived := r.PostForm.Get("archived") == "1"
	a.writeMu.Lock()
	err := a.store.Archive(c.ID, revision, archived)
	a.writeMu.Unlock()
	if errors.Is(err, ErrConflict) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	target := fmt.Sprintf("/assets/%d?restored=1", c.ID)
	if archived {
		target = "/assets/new?undone=1"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *App) list(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	archived := r.URL.Query().Get("archived") == "1"
	number, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if number < 1 {
		number = 1
	}
	if number > 1000000 {
		number = 1000000
	}
	assets, count, err := a.store.List(query, archived, 50, (number-1)*50)
	if err != nil {
		a.fail(w, err)
		return
	}
	p := page{Page: "list", Title: "Assets", Assets: assets, Query: query, Count: count, Archived: archived}
	if archived {
		p.Title = "Archive"
	}
	link := func(n int) string {
		v := url.Values{"q": {query}, "page": {strconv.Itoa(n)}}
		if archived {
			v.Set("archived", "1")
		}
		return "/assets?" + v.Encode()
	}
	if number > 1 {
		p.Previous = link(number - 1)
	}
	if number*50 < count {
		p.Next = link(number + 1)
	}
	a.render(w, http.StatusOK, p)
}

func (a *App) export(w http.ResponseWriter, r *http.Request) {
	format := r.PathValue("format")
	if format != "assets.json" && format != "assets.csv" {
		http.NotFound(w, r)
		return
	}
	assets, _, err := a.store.List("", r.URL.Query().Get("archived") == "1", -1, 0)
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+format+`"`)
	if format == "assets.json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(assets); err != nil {
			log.Printf("export: %v", err)
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	out := csv.NewWriter(w)
	_ = out.Write([]string{"id", "label", "description", "location", "photos", "created_at", "updated_at", "archived", "catalog_number", "short_description", "details"})
	for _, c := range assets {
		paths := []string{}
		for _, p := range c.Photos {
			paths = append(paths, p.Path)
		}
		row := []string{strconv.FormatInt(c.ID, 10), c.Label(), c.Description, c.Location, strings.Join(paths, ";"), c.CreatedAt, c.UpdatedAt, strconv.FormatBool(c.Archived), strconv.FormatInt(c.CatalogNumber, 10), c.ShortDescription, c.Details}
		// Keep spreadsheet programs from interpreting freeform observations as formulas.
		for i, value := range row {
			trimmed := strings.TrimLeft(value, " \t\r\n")
			if (len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0]))) || strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") {
				row[i] = "'" + value
			}
		}
		_ = out.Write(row)
	}
	out.Flush()
	if err := out.Error(); err != nil {
		log.Printf("export: %v", err)
	}
}

func (a *App) photo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if filepath.Base(name) != name || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	ext := filepath.Ext(name)
	if ext != ".jpg" && ext != ".png" && ext != ".gif" {
		http.NotFound(w, r)
		return
	}
	folder := "photos"
	if strings.HasPrefix(r.URL.Path, "/thumbnails/") {
		folder = "thumbnails"
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, filepath.Join(a.dir, folder, name))
}
