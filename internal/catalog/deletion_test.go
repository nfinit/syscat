package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestPhotoDeletionSaveAndHistory(t *testing.T) {
	app, b, dir := start(t)
	data := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Deletion test"}, "group_photos": {"Exterior", "Interior"}}, data, data, data), 303)
	original, _ := app.store.Get(1)
	var intake Asset
	if err := json.Unmarshal(original.Intake, &intake); err != nil {
		t.Fatal(err)
	}
	// Delete the overview and the only Interior photo; Exterior becomes overview.
	expect(t, b.post("/assets/1", url.Values{"revision": {"1"}, "description": {"Deletion test"}, "delete_photo": {original.Photos[0].Path, original.Photos[2].Path}, "overview_choice": {original.Photos[0].Path}}), 303)
	current, _ := app.store.Get(1)
	if len(current.Photos) != 1 || current.Photos[0].Path != original.Photos[1].Path || current.Photos[0].Group != "" {
		t.Fatal("deletion/promotion/history incorrect", current)
	}
	for _, p := range original.Photos {
		for _, file := range []string{p.Path, p.Thumbnail} {
			_, err := os.Stat(filepath.Join(dir, file))
			if p.Path == original.Photos[1].Path {
				if err != nil {
					t.Fatal("remaining image removed", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("deleted intake image retained", err)
			}
		}
	}
	var updatedIntake Asset
	if err := json.Unmarshal(current.Intake, &updatedIntake); err != nil {
		t.Fatal(err)
	}
	if len(updatedIntake.Photos) != 1 || updatedIntake.Photos[0] != intake.Photos[1] || updatedIntake.Description != intake.Description || updatedIntake.Location != intake.Location || updatedIntake.CreatedAt != intake.CreatedAt {
		t.Fatal("intake photo pruning changed other observations")
	}
	for _, p := range []Photo{original.Photos[0], original.Photos[2]} {
		for _, file := range []string{p.Path, p.Thumbnail} {
			expect(t, b.get("/"+file), 404)
		}
	}
	var api apiAsset
	if err := json.Unmarshal(b.get("/api/assets/1").Body.Bytes(), &api); err != nil {
		t.Fatal(err)
	}
	if len(api.Photos) != 1 || api.Photos[0].OriginalURL != "/"+original.Photos[1].Path {
		t.Fatal("API still includes deleted photos")
	}
	// Add a later photo, then delete it. Its files have no historical references.
	expect(t, b.upload("/assets/1", url.Values{"revision": {"2"}, "description": {"Deletion test"}}, data), 303)
	current, _ = app.store.Get(1)
	added := current.Photos[1]
	expect(t, b.post("/assets/1", url.Values{"revision": {"3"}, "description": {"Deletion test"}, "delete_photo": {added.Path}}), 303)
	for _, file := range []string{added.Path, added.Thumbnail} {
		if _, err := os.Stat(filepath.Join(dir, file)); !os.IsNotExist(err) {
			t.Fatal("unreferenced file retained", file, err)
		}
	}
	// A replacement may be uploaded while deleting the last existing photo.
	expect(t, b.upload("/assets/1", url.Values{"revision": {"4"}, "description": {"Deletion test"}, "delete_photo": {original.Photos[1].Path}, "overview_choice": {"upload:0"}}, data), 303)
	current, _ = app.store.Get(1)
	var finalIntake Asset
	if err := json.Unmarshal(current.Intake, &finalIntake); err != nil {
		t.Fatal(err)
	}
	if len(finalIntake.Photos) != 0 || finalIntake.Description != intake.Description {
		t.Fatal("final intake still references deleted photos")
	}
	if len(current.Photos) != 1 || current.Photos[0].Path == original.Photos[1].Path || current.Photos[0].Group != "" {
		t.Fatal("replacement failed")
	}
	app.Close()
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, err := reopened.store.Get(1)
	if err != nil || len(saved.Photos) != 1 || saved.Photos[0] != current.Photos[0] || string(saved.Intake) != string(current.Intake) {
		t.Fatal("deletion not persisted", err)
	}
}

func TestPhotoDeletionValidationAndConflicts(t *testing.T) {
	app, b, dir := start(t)
	data := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Atomic deletion"}}, data, data), 303)
	original, _ := app.store.Get(1)
	cases := []struct {
		values url.Values
		status int
	}{
		{url.Values{"delete_photo": {"photos/not-attached.jpg"}}, 400},
		{url.Values{"delete_photo": {original.Photos[0].Path, original.Photos[0].Path}}, 400},
		{url.Values{"delete_photo": {original.Photos[0].Path, original.Photos[1].Path}}, 400},
		{url.Values{"delete_photo": {original.Photos[1].Path}, "revision": {"0"}}, 409},
		{url.Values{"delete_photo": {original.Photos[1].Path}, "overview_choice": {"not-attached"}}, 400},
	}
	for _, tc := range cases {
		tc.values.Set("description", "Atomic deletion")
		if !tc.values.Has("revision") {
			tc.values.Set("revision", "1")
		}
		expect(t, b.post("/assets/1", tc.values), tc.status)
		current, _ := app.store.Get(1)
		if current.Revision != 1 || len(current.Photos) != 2 || string(current.Intake) != string(original.Intake) {
			t.Fatal("failed deletion changed record")
		}
		for _, folder := range []string{"photos", "thumbnails"} {
			files, _ := os.ReadDir(filepath.Join(dir, folder))
			if len(files) != 2 {
				t.Fatal("failed deletion changed files")
			}
		}
	}
	// A plain HTML submission keeps complete pre-deletion positions, including gaps
	// after filtering, while preserving the explicit overview and remaining captions.
	values := url.Values{"description": {"Atomic deletion"}, "revision": {"1"}, "delete_photo": {original.Photos[1].Path}, "overview_choice": {original.Photos[0].Path}}
	for i, p := range original.Photos {
		values.Set("position_"+p.Path, fmt.Sprint(i+1))
	}
	expect(t, b.post("/assets/1", values), 303)
	current, _ := app.store.Get(1)
	if len(current.Photos) != 1 || current.Photos[0] != original.Photos[0] {
		t.Fatal("plain HTML deletion failed")
	}
}
