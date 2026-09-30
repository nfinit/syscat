package catalog

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhotoGroupsAndOverview(t *testing.T) {
	app, b, dir := start(t)
	data := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Grouped system"}, "caption_overview": {"Portrait"}, "caption_photos": {"Board", "Case", "Circuit", "Loose"}, "group_photos": {" Interior ", "Exterior", "Interior", ""}}, data, data, data, data, data), 303)
	c, _ := app.store.Get(1)
	original := string(c.Intake)
	if len(c.Photos) != 5 || c.Photos[0].Group != "" || c.Photos[1].Caption != "Board" || c.Photos[2].Caption != "Circuit" || c.Photos[3].Caption != "Case" || c.Photos[4].Caption != "Loose" {
		t.Fatal("initial grouping incorrect", c.Photos)
	}
	sections := c.PhotoSections()
	if len(sections) != 4 || !sections[0].Overview || sections[1].Name != "Interior" || sections[2].Name != "Exterior" || sections[3].Title != "Photos" {
		t.Fatal("sections incorrect", sections)
	}
	initial := append([]Photo(nil), c.Photos...)
	values := url.Values{"revision": {"1"}, "description": {"Grouped system"}, "overview_choice": {initial[3].Path}, "group_position_Interior": {"2"}, "group_position_Exterior": {"1"}, "group_" + initial[2].Path: {"Video board"}}
	expect(t, b.post("/assets/1", values), 303)
	c, _ = app.store.Get(1)
	if c.Photos[0].Path != initial[3].Path || c.Photos[0].Group != "" {
		t.Fatal("grouped photo not promoted", c.Photos)
	}
	if c.Photos[1].Group != "Interior" || c.Photos[2].Group != "Video board" {
		t.Fatal("remaining named groups incorrect", c.Photos)
	}
	if c.Photos[3].Path != initial[0].Path || c.Photos[3].Group != "" {
		t.Fatal("former overview not demoted to Photos")
	}
	if string(c.Intake) != original {
		t.Fatal("group editing changed intake")
	}
	// A newly uploaded overview clears its submitted group and keeps every photo.
	expect(t, b.upload("/assets/1", url.Values{"revision": {"2"}, "description": {"Grouped system"}, "overview_choice": {"upload:1"}, "caption_photos": {"New board", "New portrait"}, "group_photos": {"Video board", "Exterior"}}, data, data), 303)
	c, _ = app.store.Get(1)
	if len(c.Photos) != 7 || c.Photos[0].Caption != "New portrait" || c.Photos[0].Group != "" {
		t.Fatal("new overview incorrect", c.Photos)
	}
	var api apiAsset
	if err := json.Unmarshal(b.get("/api/assets/1").Body.Bytes(), &api); err != nil {
		t.Fatal(err)
	}
	if api.Photos[0].Group != "" || api.Photos[1].Group != "Interior" || api.Photos[2].Group != "Video board" {
		t.Fatal("API groups missing")
	}
	for _, route := range []string{"/assets/1", "/assets/1/edit"} {
		body := b.get(route).Body.String()
		for _, text := range []string{"Overview</h3>", "Interior</h3>", "Video board</h3>", "Photos</h3>", "group_position_Interior", "Set as overview"} {
			if !strings.Contains(body, text) {
				t.Fatal("missing section control", text, route)
			}
		}
	}
	for _, bad := range []url.Values{
		{"group_" + c.Photos[1].Path: {strings.Repeat("x", 101)}},
		{"group_" + c.Photos[1].Path: {"Bad\nname"}},
		{"group_" + c.Photos[1].Path: {"One", "Two"}},
		{"group_position_Interior": {"1"}, "group_position_Video board": {"1"}},
		{"overview_choice": {"photos/not-attached.jpg"}},
	} {
		bad.Set("revision", "3")
		bad.Set("description", "Grouped system")
		expect(t, b.upload("/assets/1", bad, data), 400)
		current, _ := app.store.Get(1)
		if current.Revision != 3 {
			t.Fatal("invalid grouping changed record")
		}
	}
	for _, folder := range []string{"photos", "thumbnails"} {
		files, _ := os.ReadDir(filepath.Join(dir, folder))
		if len(files) != 7 {
			t.Fatal("group edit leaked files", len(files))
		}
	}
	expect(t, b.post("/assets/1/archive", url.Values{"revision": {"3"}, "archived": {"1"}}), 303)
	body := b.get("/assets/1").Body.String()
	if !strings.Contains(body, "Video board</h3>") || !strings.Contains(body, "/static/photo-groups.js") {
		t.Fatal("archived grouped view missing")
	}
	app.Close()
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c, err = reopened.store.Get(1)
	if err != nil || c.Photos[2].Group != "Video board" {
		t.Fatal("group persistence failed", err)
	}
}

func TestGroupUploadValidation(t *testing.T) {
	app, b, dir := start(t)
	for _, groups := range [][]string{{strings.Repeat("x", 101)}, {"One", "Two"}} {
		expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Invalid groups"}, "group_photos": groups}, pngPhoto(t), pngPhoto(t)), 400)
	}
	_, count, err := app.store.List("", false, 50, 0)
	if err != nil || count != 0 {
		t.Fatal("invalid groups saved")
	}
	files, _ := os.ReadDir(filepath.Join(dir, "photos"))
	if len(files) != 0 {
		t.Fatal("invalid group upload left files")
	}
}
