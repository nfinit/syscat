package catalog

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhotoCaptionLifecycle(t *testing.T) {
	app, b, dir := start(t)
	data := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{
		"submission": {randomKey()}, "description": {"Captioned system"},
		"caption_overview": {" Front view "}, "caption_photos": {"Mainboard <detail>", ""},
	}, data, data, data), 303)
	c, err := app.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if c.Photos[0].Caption != "Front view" || c.Photos[1].Caption != "Mainboard <detail>" || c.Photos[2].Caption != "" {
		t.Fatal(c.Photos)
	}
	original := string(c.Intake)
	paths := append([]Photo(nil), c.Photos...)
	for _, route := range []string{"/assets/1", "/assets/1/edit"} {
		body := b.get(route).Body.String()
		if !strings.Contains(body, `value="Mainboard &lt;detail&gt;"`) || !strings.Contains(body, `name="caption_`+c.Photos[0].Path+`"`) {
			t.Fatal("caption fields missing or unescaped", body)
		}
	}
	values := url.Values{"revision": {"1"}, "description": {"Captioned system"},
		"caption_" + c.Photos[0].Path: {" Updated front "}, "caption_" + c.Photos[1].Path: {""},
		"caption_photos": {"New detail"},
	}
	expect(t, b.upload("/assets/1", values, data), 303)
	c, _ = app.store.Get(1)
	if c.Photos[0].Caption != "Updated front" || c.Photos[1].Caption != "" || c.Photos[3].Caption != "New detail" || string(c.Intake) != original {
		t.Fatal("caption edit or snapshot incorrect", c)
	}
	// Text-only clients omitting caption fields preserve the current captions.
	expect(t, b.post("/assets/1", url.Values{"revision": {"2"}, "description": {"Captioned system"}}), 303)
	c, _ = app.store.Get(1)
	if c.Photos[0].Caption != "Updated front" {
		t.Fatal("omitted caption cleared existing value")
	}
	// Stale edits must not change metadata or files.
	expect(t, b.post("/assets/1", url.Values{"revision": {"2"}, "description": {"Captioned system"}, "caption_" + c.Photos[0].Path: {"Stale"}}), 409)
	// Validation failures retain the submitted caption for correction.
	result := b.post("/assets/1", url.Values{"revision": {"3"}, "description": {""}, "caption_" + c.Photos[0].Path: {"Unsaved caption"}})
	expect(t, result, 400)
	if !strings.Contains(result.Body.String(), `value="Unsaved caption"`) {
		t.Fatal("caption lost on validation failure")
	}
	expect(t, b.post("/assets/1", url.Values{"revision": {"3"}, "description": {"Captioned system"}, "caption_" + c.Photos[0].Path: {strings.Repeat("x", 1001)}}), 400)
	c, _ = app.store.Get(1)
	if c.Revision != 3 || c.Photos[0].Caption != "Updated front" {
		t.Fatal("invalid save changed stored captions")
	}
	for i, p := range paths {
		if c.Photos[i].Path != p.Path || c.Photos[i].Thumbnail != p.Thumbnail {
			t.Fatal("caption edit changed image references")
		}
		bytes, err := os.ReadFile(filepath.Join(dir, p.Path))
		if err != nil || string(bytes) != string(data) {
			t.Fatal("caption edit changed original image", err)
		}
	}
	var detail apiAsset
	if err := json.Unmarshal(b.get("/api/assets/1").Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Photos[0].Caption != "Updated front" || detail.Photos[3].Caption != "New detail" {
		t.Fatal("API captions missing")
	}
	var list apiAssetList
	if err := json.Unmarshal(b.get("/api/assets").Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Assets[0].Photos[0].Caption != "Updated front" {
		t.Fatal("list API caption missing")
	}
	var exported []Asset
	if err := json.Unmarshal(b.get("/export/assets.json").Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if exported[0].Photos[3].Caption != "New detail" {
		t.Fatal("JSON export caption missing")
	}
	expect(t, b.post("/assets/1/archive", url.Values{"revision": {"3"}, "archived": {"1"}}), 303)
	body := b.get("/assets/1").Body.String()
	if !strings.Contains(body, "<figcaption>Updated front") {
		t.Fatal("archived caption missing")
	}
	// Reopening the inventory preserves captions.
	app.Close()
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c, err = reopened.store.Get(1)
	if err != nil || c.Photos[3].Caption != "New detail" {
		t.Fatal("caption persistence failed", err)
	}
}

func TestInvalidUploadCaptionsLeaveNoFiles(t *testing.T) {
	app, b, dir := start(t)
	for _, captions := range []url.Values{
		{"caption_overview": {strings.Repeat("x", 1001)}},
		{"caption_photos": {"One", "Extra"}},
	} {
		captions.Set("submission", randomKey())
		captions.Set("description", "Rejected caption")
		expect(t, b.upload("/assets", captions, pngPhoto(t), pngPhoto(t)), 400)
		for _, folder := range []string{"photos", "thumbnails"} {
			files, err := os.ReadDir(filepath.Join(dir, folder))
			if err != nil || len(files) != 0 {
				t.Fatal("invalid caption left files", err)
			}
		}
	}
	_, total, err := app.store.List("", false, 50, 0)
	if err != nil || total != 0 {
		t.Fatal("invalid caption created record", err)
	}
	// Old inventories without caption keys remain readable.
	var photos []Photo
	if err := json.Unmarshal([]byte(`[{"path":"photos/old.jpg","thumbnail":"thumbnails/old.jpg","original_name":"old.jpg"}]`), &photos); err != nil || photos[0].Caption != "" {
		t.Fatal("legacy photo compatibility", err)
	}
}
