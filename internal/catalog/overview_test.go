package catalog

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestOverviewPromotionRetainsPhotos(t *testing.T) {
	app, b, dir := start(t)
	data := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Overview system"}, "caption_overview": {"Portrait"}, "caption_photos": {"Board", "Rear"}, "overview_choice": {"upload:2"}}, data, data, data), 303)
	c, _ := app.store.Get(1)
	if c.Photos[0].Caption != "Rear" || c.Photos[1].Caption != "Portrait" || c.Photos[2].Caption != "Board" {
		t.Fatal("initial overview selection incorrect", c.Photos)
	}
	original := string(c.Intake)
	initial := append([]Photo(nil), c.Photos...)
	expect(t, b.post("/assets/1", url.Values{"revision": {"1"}, "description": {"Overview system"}, "overview_choice": {initial[2].Path}}), 303)
	c, _ = app.store.Get(1)
	if c.Photos[0] != initial[2] || c.Photos[1] != initial[0] || c.Photos[2] != initial[1] {
		t.Fatal("overview selection did not preserve detail order", c.Photos)
	}
	if string(c.Intake) != original {
		t.Fatal("selection changed intake")
	}
	// Invalid selections and stale edits leave both metadata and files intact.
	for _, tc := range []struct {
		choice   []string
		revision string
		status   int
	}{
		{[]string{"photos/not-attached.jpg"}, "2", 400},
		{[]string{initial[0].Path, initial[1].Path}, "2", 400},
		{[]string{"upload:-1"}, "2", 400},
		{[]string{"upload:1"}, "2", 400},
		{[]string{"upload:01"}, "2", 400},
		{[]string{"upload:0"}, "1", 409},
	} {
		expect(t, b.upload("/assets/1", url.Values{"description": {"Overview system"}, "revision": {tc.revision}, "overview_choice": tc.choice}, data), tc.status)
		current, _ := app.store.Get(1)
		if current.Revision != 2 || current.Photos[0] != initial[2] {
			t.Fatal("invalid action changed overview")
		}
		files, _ := os.ReadDir(filepath.Join(dir, "photos"))
		if len(files) != 3 {
			t.Fatal("invalid action leaked or deleted files")
		}
	}
	expect(t, b.upload("/assets/1", url.Values{"description": {"Overview system"}, "revision": {"2"}, "overview_choice": {"upload:0"}}, []byte("invalid image")), 400)
	// Promote the second newly uploaded photo, retaining its own caption and all
	// previous photos, including the former overview.
	expect(t, b.upload("/assets/1", url.Values{"revision": {"2"}, "description": {"Overview system"}, "overview_choice": {"upload:1"}, "caption_photos": {"New detail", "New portrait"}}, data, data), 303)
	c, _ = app.store.Get(1)
	if len(c.Photos) != 5 || c.Photos[0].Caption != "New portrait" || c.Photos[1] != initial[2] || c.Photos[2] != initial[0] || c.Photos[3] != initial[1] || c.Photos[4].Caption != "New detail" {
		t.Fatal("new overview promotion incorrect", c.Photos)
	}
	if string(c.Intake) != original {
		t.Fatal("promotion changed original intake")
	}
	var api apiAsset
	if err := json.Unmarshal(b.get("/api/assets/1").Body.Bytes(), &api); err != nil {
		t.Fatal(err)
	}
	if api.Photos[0].Role != "overview" || api.Photos[0].Caption != "New portrait" || api.Photos[1].Role != "detail" {
		t.Fatal("API overview incorrect")
	}
	// Adding photos without changing the overview preserves its position.
	selected := c.Photos[0]
	expect(t, b.upload("/assets/1", url.Values{"description": {"Overview system"}, "revision": {"3"}, "overview_choice": {selected.Path}}, data), 303)
	c, _ = app.store.Get(1)
	if len(c.Photos) != 6 || c.Photos[0] != selected {
		t.Fatal("normal upload changed overview")
	}
	for _, p := range c.Photos {
		for _, path := range []string{p.Path, p.Thumbnail} {
			if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
				t.Fatal("attached photo removed", err)
			}
		}
	}
	for _, folder := range []string{"photos", "thumbnails"} {
		files, _ := os.ReadDir(filepath.Join(dir, folder))
		if len(files) != 6 {
			t.Fatal("unexpected file count", folder, len(files))
		}
	}
}
