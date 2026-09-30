package catalog

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

type browser struct {
	app    *App
	cookie *http.Cookie
	csrf   string
}

func start(t *testing.T) (*App, *browser, string) {
	t.Helper()
	dir := t.TempDir()
	app, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	b := &browser{app: app}
	result := b.get("/assets/new")
	if result.Code != 200 {
		t.Fatalf("intake: %d %s", result.Code, result.Body.String())
	}
	b.cookie = result.Result().Cookies()[0]
	b.csrf = hidden(result.Body.String(), "csrf")
	return app, b, dir
}

func hidden(body, name string) string {
	match := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func (b *browser) request(method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if b.cookie != nil {
		req.AddCookie(b.cookie)
	}
	result := httptest.NewRecorder()
	b.app.ServeHTTP(result, req)
	return result
}
func (b *browser) get(path string) *httptest.ResponseRecorder { return b.request("GET", path, "", nil) }
func (b *browser) post(path string, values url.Values) *httptest.ResponseRecorder {
	if values.Get("csrf") == "" {
		values.Set("csrf", b.csrf)
	}
	return b.request("POST", path, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
}
func (b *browser) upload(path string, values url.Values, files ...[]byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	values.Set("csrf", b.csrf)
	for key, items := range values {
		for _, value := range items {
			_ = writer.WriteField(key, value)
		}
	}
	for i, file := range files {
		part, _ := writer.CreateFormFile("photos", fmt.Sprintf("photo-%d.jpg", i))
		_, _ = part.Write(file)
	}
	_ = writer.Close()
	return b.request("POST", path, writer.FormDataContentType(), &body)
}

func expect(t *testing.T, r *httptest.ResponseRecorder, status int) {
	t.Helper()
	if r.Code != status {
		t.Fatalf("got HTTP %d, want %d: %s", r.Code, status, r.Body.String())
	}
}

func TestIntakeEditUndoAndPersistence(t *testing.T) {
	app, b, dir := start(t)
	key := hidden(b.get("/assets/new").Body.String(), "submission")
	values := url.Values{"submission": {key}, "description": {"Mystery <desktop> system\nPossibly a 486"}, "location": {"Shelf B / 12"}}
	result := b.post("/assets", values)
	expect(t, result, 303)
	if result.Header().Get("Location") != "/assets/new?saved=1" {
		t.Fatal(result.Header())
	}
	next := b.get(result.Header().Get("Location"))
	expect(t, next, 200)
	for _, text := range []string{"00001", "value=\"Shelf B / 12\"", "Undo entry", "Mystery &lt;desktop&gt; system"} {
		if !strings.Contains(next.Body.String(), text) {
			t.Errorf("missing %q", text)
		}
	}
	expect(t, b.post("/assets", values), 303)
	_, count, err := app.store.List("", false, 50, 0)
	if err != nil || count != 1 {
		t.Fatalf("duplicate save count %d: %v", count, err)
	}
	c, err := app.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, b.get("/assets/1"), 200)
	expect(t, b.get("/assets/1/edit"), 200)
	edit := url.Values{"description": {"Identified desktop\nConfirmed 486"}, "location": {"Shelf C"}, "revision": {"1"}}
	expect(t, b.post("/assets/1", edit), 303)
	edit.Set("description", "Stale description")
	conflict := b.post("/assets/1", edit)
	expect(t, conflict, 409)
	if !strings.Contains(conflict.Body.String(), "Stale description") {
		t.Fatal("lost conflicting text")
	}
	c, err = app.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if c.Description != "Identified desktop\nConfirmed 486" {
		t.Fatal(c)
	}
	var original Asset
	if err := json.Unmarshal(c.Intake, &original); err != nil {
		t.Fatal(err)
	}
	if original.Description != "Mystery <desktop> system\nPossibly a 486" {
		t.Fatal("original description overwritten")
	}
	expect(t, b.post("/assets/1/archive", url.Values{"revision": {"1"}, "archived": {"1"}}), 409)
	expect(t, b.post("/assets/1/archive", url.Values{"revision": {"2"}, "archived": {"1"}}), 303)
	_, count, _ = app.store.List("", false, 50, 0)
	if count != 0 {
		t.Fatal("archive remained active")
	}
	expect(t, b.get("/assets?archived=1"), 200)
	expect(t, b.post("/assets/1/archive", url.Values{"revision": {"3"}, "archived": {"0"}}), 303)
	expect(t, b.post("/assets", url.Values{"submission": {randomKey()}}), 303)
	second, _ := app.store.Get(2)
	if second.ID != 2 {
		t.Fatal("ID not allocated")
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c, err = reopened.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if c.Archived || c.Description != "Identified desktop\nConfirmed 486" || c.Revision != 4 {
		t.Fatalf("bad persisted record: %+v", c)
	}
}

func TestConcurrentDuplicateSave(t *testing.T) {
	app, b, _ := start(t)
	key := randomKey()
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := b.post("/assets", url.Values{"submission": {key}, "description": {"Same physical system"}})
			if result.Code != 303 {
				t.Errorf("save: %d", result.Code)
			}
		}()
	}
	wg.Wait()
	_, count, err := app.store.List("", false, 50, 0)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestFormsRejectMissingCSRFAndRetainInvalidInput(t *testing.T) {
	app, b, _ := start(t)
	bad := b.post("/assets", url.Values{"csrf": {"wrong"}, "submission": {randomKey()}, "description": {"My unsaved notes"}})
	expect(t, bad, 400)
	if !strings.Contains(bad.Body.String(), "My unsaved notes") {
		t.Fatal("text lost")
	}
	tooLong := b.post("/assets", url.Values{"submission": {randomKey()}, "location": {strings.Repeat("x", 501)}})
	expect(t, tooLong, 400)
	_, count, _ := app.store.List("", false, 50, 0)
	if count != 0 {
		t.Fatal("invalid entry saved")
	}
	for _, path := range []string{"/assets/nope", "/assets/999", "/photos/syscat.sqlite3", "/thumbnails/.hidden.jpg", "/export/nope"} {
		expect(t, b.get(path), 404)
	}
}

func TestSearchPaginationAndExports(t *testing.T) {
	app, b, _ := start(t)
	for i := range 53 {
		_, err := app.store.Create(Asset{Description: fmt.Sprintf("System %d", i)}, randomKey())
		if err != nil {
			t.Fatal(err)
		}
	}
	id, err := app.store.Create(Asset{Description: "=SUM(1,2)\n100% _ literal"}, randomKey())
	if err != nil {
		t.Fatal(err)
	}
	assets, count, err := app.store.List("% _", false, 50, 0)
	if err != nil || count != 1 || assets[0].ID != id {
		t.Fatalf("literal search: %v %d %v", assets, count, err)
	}
	assets, count, err = app.store.List("00054", false, 50, 0)
	if err != nil || count != 1 || len(assets) != 1 {
		t.Fatalf("ID search: %v", err)
	}
	first := b.get("/assets")
	expect(t, first, 200)
	if !strings.Contains(first.Body.String(), "Next") {
		t.Fatal("missing next page")
	}
	expect(t, b.get("/assets?page=2"), 200)
	expect(t, b.get("/assets?q=missing"), 200)
	result := b.get("/export/assets.json")
	expect(t, result, 200)
	var exported []Asset
	if err := json.Unmarshal(result.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported) != 54 || exported[0].Description != "=SUM(1,2)\n100% _ literal" {
		t.Fatal("JSON export was not lossless")
	}
	result = b.get("/export/assets.csv")
	expect(t, result, 200)
	rows, err := csv.NewReader(result.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 55 || rows[1][2] != "'=SUM(1,2)\n100% _ literal" {
		t.Fatal("CSV output incorrect")
	}
}

func pngPhoto(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPhotoUploadRollbackAndLaterAttachment(t *testing.T) {
	app, b, dir := start(t)
	data := pngPhoto(t)
	result := b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Photo system"}}, data, []byte("not an image"))
	expect(t, result, 400)
	if !strings.Contains(result.Body.String(), "Photo system") {
		t.Fatal("photo error lost text")
	}
	files, _ := os.ReadDir(filepath.Join(dir, "photos"))
	if len(files) != 0 {
		t.Fatal("failed upload left original behind")
	}
	files, _ = os.ReadDir(filepath.Join(dir, "thumbnails"))
	if len(files) != 0 {
		t.Fatal("failed upload left thumbnail behind")
	}
	key := randomKey()
	values := url.Values{"submission": {key}, "description": {"Photo system"}}
	expect(t, b.upload("/assets", values, data), 303)
	expect(t, b.upload("/assets", values, data), 303)
	c, err := app.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Photos) != 1 {
		t.Fatal(c.Photos)
	}
	original, err := os.ReadFile(filepath.Join(dir, c.Photos[0].Path))
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("original not preserved", err)
	}
	expect(t, b.get("/"+c.Photos[0].Path), 200)
	expect(t, b.get("/"+c.Photos[0].Thumbnail), 200)
	expect(t, b.upload("/assets/1", url.Values{"revision": {"1"}, "description": {"Photo system"}}, data), 303)
	c, _ = app.store.Get(1)
	if len(c.Photos) != 2 {
		t.Fatal("later photo missing")
	}
	files, _ = os.ReadDir(filepath.Join(dir, "photos"))
	if len(files) != 2 {
		t.Fatal("duplicate request leaked files")
	}
	expect(t, b.upload("/assets/1", url.Values{"revision": {"1"}}, data), 409)
	files, _ = os.ReadDir(filepath.Join(dir, "photos"))
	if len(files) != 2 {
		t.Fatal("conflicting edit leaked files")
	}
}

func TestPhotoEXIFOrientation(t *testing.T) {
	app, b, dir := start(t)
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil); err != nil {
		t.Fatal(err)
	}
	// JPEG APP1: little-endian TIFF with orientation 6 (90 degrees clockwise).
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	oriented := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	oriented = append(oriented, raw.Bytes()[2:]...)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}}, oriented), 303)
	c, _ := app.store.Get(1)
	file, err := os.Open(filepath.Join(dir, c.Photos[0].Thumbnail))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 20 || cfg.Height != 40 {
		t.Fatalf("thumbnail orientation: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestRejectNewerSchema(t *testing.T) {
	app, _, dir := start(t)
	if _, err := app.store.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	app.Close()
	newer, err := New(dir)
	if err == nil {
		newer.Close()
		t.Fatal("accepted unsupported schema")
	}
	if !strings.Contains(err.Error(), "newer") {
		t.Fatal(err)
	}
}

func TestSystemIntakeFormAndDetailedDescription(t *testing.T) {
	app, b, _ := start(t)
	form := b.get("/assets/new").Body.String()
	for _, text := range []string{"Syscat", "Overview photo", "Detail photos", "multiple", "<textarea", "Inventory ID assigned"} {
		if !strings.Contains(form, text) {
			t.Errorf("missing %q", text)
		}
	}
	for _, field := range []string{"bus", "category", "notes"} {
		if strings.Contains(form, `name="`+field+`"`) {
			t.Errorf("unexpected classification field %s", field)
		}
	}
	description := "Unknown desktop\n" + strings.TrimSpace(strings.Repeat("Detailed observation. ", 400))
	expect(t, b.post("/assets", url.Values{"submission": {randomKey()}, "description": {description}}), 303)
	c, err := app.store.Get(1)
	if err != nil || c.Description != description || c.Title() != "Unknown desktop" || c.Label() != "00001" {
		t.Fatalf("intake mismatch: id=%d, description bytes=%d, title=%q, error=%v", c.ID, len(c.Description), c.Title(), err)
	}
	expect(t, b.post("/assets", url.Values{"submission": {randomKey()}, "description": {strings.Repeat("x", 20001)}}), 400)
	long := Asset{ID: 100000, Description: strings.Repeat("x", 150)}
	if long.Label() != "100000" || len(long.Title()) != 123 {
		t.Fatal("label or title truncation incorrect")
	}
}

func TestUnprefixedInventorySearch(t *testing.T) {
	app, _, _ := start(t)
	for range 154 {
		if _, err := app.store.Create(Asset{}, randomKey()); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"54", "00054"} {
		assets, count, err := app.store.List(query, false, 50, 0)
		if err != nil || count != 1 || len(assets) != 1 || assets[0].ID != 54 {
			t.Fatalf("search %q: %v, count=%d, error=%v", query, assets, count, err)
		}
	}
}

func TestOpenEndedPhotoBatches(t *testing.T) {
	app, b, dir := start(t)
	data := pngPhoto(t)
	batch := make([][]byte, 12)
	for i := range batch {
		batch[i] = data
	}
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}}, batch...), 303)
	c, err := app.store.Get(1)
	if err != nil || len(c.Photos) != 12 {
		t.Fatalf("initial batch: %d %v", len(c.Photos), err)
	}
	first := c.Photos[0].Path
	expect(t, b.upload("/assets/1", url.Values{"revision": {"1"}}, batch...), 303)
	c, err = app.store.Get(1)
	if err != nil || len(c.Photos) != 24 || c.Photos[0].Path != first {
		t.Fatalf("followup batch: %+v %v", c, err)
	}
	var original Asset
	if err := json.Unmarshal(c.Intake, &original); err != nil || len(original.Photos) != 12 {
		t.Fatal("original photo snapshot changed", err)
	}
	// An invalid file after a large valid batch must roll the entire batch back.
	batch = append(batch, []byte("invalid photo"))
	expect(t, b.upload("/assets/1", url.Values{"revision": {"2"}}, batch...), 400)
	for _, folder := range []string{"photos", "thumbnails"} {
		files, err := os.ReadDir(filepath.Join(dir, folder))
		if err != nil || len(files) != 24 {
			t.Fatalf("rollback in %s: files=%d error=%v", folder, len(files), err)
		}
	}
}
