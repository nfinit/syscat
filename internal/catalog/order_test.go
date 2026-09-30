package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestPhotoOrderPreservesPhotosAndCaptions(t *testing.T) {
	app, b, dir := start(t)
	data := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Order test"}, "caption_overview": {"Front"}, "caption_photos": {"Board", "Rear"}}, data, data, data), 303)
	c, _ := app.store.Get(1)
	initial := append([]Photo(nil), c.Photos...)
	intake := string(c.Intake)
	values := url.Values{"revision": {"1"}, "description": {"Order test"}, "overview_choice": {initial[1].Path}, "position_" + initial[0].Path: {"3"}, "position_" + initial[1].Path: {"1"}, "position_" + initial[2].Path: {"2"}, "caption_" + initial[2].Path: {"Rear ports"}, "caption_photos": {"New detail"}}
	// Overview selection is independent of ordering within sections.
	expect(t, b.post("/assets/1", values), 303)
	c, _ = app.store.Get(1)
	if c.Photos[0] != initial[1] || c.Photos[1].Path != initial[2].Path || c.Photos[1].Caption != "Rear ports" || c.Photos[2] != initial[0] {
		t.Fatal("order/captions incorrect", c.Photos)
	}
	values.Set("revision", "2")
	expect(t, b.upload("/assets/1", values, data), 303)
	c, _ = app.store.Get(1)
	if len(c.Photos) != 4 || c.Photos[0] != initial[1] || c.Photos[1].Path != initial[2].Path || c.Photos[2] != initial[0] || c.Photos[3].Caption != "New detail" {
		t.Fatal("order with appended upload incorrect", c.Photos)
	}
	if string(c.Intake) != intake {
		t.Fatal("ordering changed intake")
	}
	saved := append([]Photo(nil), c.Photos...)
	// Partial, duplicate, repeated and invalid positions are rejected atomically.
	for _, bad := range []url.Values{
		{"position_" + saved[0].Path: {"2"}},
		{"position_" + saved[0].Path: {"0"}},
		{"position_" + saved[0].Path: {"1", "2"}},
		{"position_" + saved[0].Path: {"1"}, "position_" + saved[1].Path: {"1"}, "position_" + saved[2].Path: {"3"}, "position_" + saved[3].Path: {"4"}},
	} {
		bad.Set("revision", "3")
		bad.Set("description", "Order test")
		expect(t, b.upload("/assets/1", bad, data), 400)
		current, _ := app.store.Get(1)
		if current.Revision != 3 {
			t.Fatal("invalid order changed revision")
		}
		for i, p := range current.Photos {
			if p != saved[i] {
				t.Fatal("invalid order changed photos")
			}
		}
	}
	// Stale position edits are rejected before any upload is written.
	stale := url.Values{"revision": {"1"}, "description": {"Order test"}}
	for i, p := range saved {
		stale.Set("position_"+p.Path, fmt.Sprint(i+1))
	}
	expect(t, b.upload("/assets/1", stale, data), 409)
	for _, folder := range []string{"photos", "thumbnails"} {
		files, _ := os.ReadDir(filepath.Join(dir, folder))
		if len(files) != 4 {
			t.Fatal("ordering leaked or removed files", len(files))
		}
	}
	var api apiAsset
	if err := json.Unmarshal(b.get("/api/assets/1").Body.Bytes(), &api); err != nil {
		t.Fatal(err)
	}
	for i, p := range api.Photos {
		if p.OriginalURL != "/"+saved[i].Path || p.Caption != saved[i].Caption {
			t.Fatal("API order incorrect")
		}
	}
	app.Close()
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c, err = reopened.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range c.Photos {
		if p != saved[i] {
			t.Fatal("ordering not persisted")
		}
	}
}
