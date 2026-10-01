package catalog

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAPIWriteLifecycleAndStablePhotoIDs(t *testing.T) {
	app, b, dir := start(t)
	image := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Initial system"}, "location": {"Workshop"}, "caption_photos": {"Unidentified chip", "Rear"}, "group_photos": {"Interior", "Exterior"}}, image, image, image), 303)
	initial, _ := app.store.Get(1)
	intake := string(initial.Intake)
	browserCookie := b.cookie
	b.cookie = nil
	sessions := len(app.sessions)
	var before apiAsset
	readAPI(t, b.get("/api/assets/1"), &before)
	if before.Photos[1].ID == "" || before.Photos[1].APIURL != "/api/assets/1/photos/"+before.Photos[1].ID {
		t.Fatal("missing stable photo address")
	}
	patch := func(path, body string, want int) *apiAsset {
		t.Helper()
		response := b.request("PATCH", path, "application/json", strings.NewReader(body))
		expect(t, response, want)
		if want != 200 {
			return nil
		}
		var asset apiAsset
		readAPI(t, response, &asset)
		return &asset
	}
	updated := patch("/api/assets/1", `{"revision":1,"description":"  Researched system\nIdentified motherboard.  "}`, 200)
	if updated.Revision != 2 || updated.Description != "Researched system\nIdentified motherboard." || updated.Location != "Workshop" || string(updated.Intake) != intake {
		t.Fatal("asset PATCH changed omitted fields or intake")
	}
	updated = patch(before.Photos[1].APIURL, `{"revision":2,"caption":"  Identified Super I/O controller  ","group":"Exterior"}`, 200)
	if updated.Revision != 3 || len(updated.Photos) != 3 || updated.Photos[0].ID != before.Photos[0].ID {
		t.Fatal("photo edit changed overview or count")
	}
	for _, photo := range updated.Photos {
		if photo.ID == before.Photos[1].ID && (photo.Caption != "Identified Super I/O controller" || photo.Group != "Exterior" || photo.OriginalURL != before.Photos[1].OriginalURL) {
			t.Fatal("photo identity/edit incorrect")
		}
	}
	// Clearing optional text is explicit; omitted caption remains intact.
	updated = patch(before.Photos[1].APIURL, `{"revision":3,"group":""}`, 200)
	updated = patch("/api/assets/1", `{"revision":4,"location":""}`, 200)
	if updated.Revision != 5 || updated.Location != "" || string(updated.Intake) != intake {
		t.Fatal("clear location failed")
	}
	// Browser edits share revision protection with API writes.
	if len(app.sessions) != sessions {
		t.Fatal("API writes created sessions")
	}
	b.cookie = browserCookie
	expect(t, b.post("/assets/1", url.Values{"revision": {"4"}, "description": {"Stale browser"}}), 409)
	expect(t, b.post("/assets/1", url.Values{"revision": {"5"}, "description": {"Browser update"}}), 303)
	patch("/api/assets/1", `{"revision":5,"description":"Stale agent"}`, 409)
	current, _ := app.store.Get(1)
	if current.Revision != 6 || current.Description != "Browser update" || current.Photos[2].ID() != before.Photos[1].ID || current.Photos[2].Caption != "Identified Super I/O controller" {
		t.Fatal("group normalization or interleaved edits incorrect")
	}
	for _, photo := range initial.Photos {
		for _, path := range []string{photo.Path, photo.Thumbnail} {
			if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
				t.Fatal("API edit removed image", err)
			}
		}
	}
	// API writes themselves do not create sessions (the browser post above does).
	b.cookie = nil
	sessions = len(app.sessions)
	patch(before.Photos[1].APIURL, `{"revision":6,"caption":""}`, 200)
	if len(app.sessions) != sessions {
		t.Fatal("API write created session")
	}
	app.Close()
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, err := reopened.store.Get(1)
	if err != nil || saved.Revision != 7 || saved.Photos[2].ID() != before.Photos[1].ID || saved.Photos[2].Caption != "" || string(saved.Intake) != intake {
		t.Fatal("writes did not persist", err)
	}
}

func TestAPIWriteRejectsInvalidEditsAtomically(t *testing.T) {
	app, b, _ := start(t)
	image := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Original"}}, image, image), 303)
	initial, _ := app.store.Get(1)
	photoURL := fmt.Sprintf("/api/assets/1/photos/%s", initial.Photos[1].ID())
	cases := []struct {
		path, contentType, body string
		status                  int
	}{
		{"/api/assets/1", "application/json", `{"description":"Missing revision"}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"description":""}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"location":null}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"location":123}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"revision":1,"location":"X"}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"location":"X","location":"Y"}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"archived":true}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"photos":[]}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1.5,"location":"X"}`, 400},
		{"/api/assets/1", "application/json", `{"revision":0,"location":"X"}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"location":"X"} {}`, 400},
		{"/api/assets/1", "application/json", `[]`, 400},
		{"/api/assets/1", "text/plain", `{"revision":1,"location":"X"}`, 415},
		{"/api/assets/1?revision=1", "application/json", `{"revision":1,"location":"X"}`, 400},
		{"/api/assets/0", "application/json", `{"revision":1,"location":"X"}`, 400},
		{"/api/assets/999", "application/json", `{"revision":1,"location":"X"}`, 404},
		{"/api/assets/1/photos/missing", "application/json", `{"revision":1,"caption":"X"}`, 404},
		{photoURL, "application/json", `{"revision":1,"group":"line\nbreak"}`, 400},
		{photoURL, "application/json", `{"revision":1,"caption":"X","original_url":"/other"}`, 400},
		{"/api/assets/1/photos/" + initial.Photos[0].ID(), "application/json", `{"revision":1,"group":"Interior"}`, 400},
		{photoURL, "application/json", `{"revision":1,"caption":"` + strings.Repeat("é", 1001) + `"}`, 400},
		{photoURL, "application/json", `{"revision":1,"group":"` + strings.Repeat("x", 101) + `"}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"location":"` + strings.Repeat("x", 501) + `"}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"description":"` + strings.Repeat("x", 20001) + `"}`, 400},
		{"/api/assets/1", "application/json", `{"revision":1,"description":"` + strings.Repeat("x", maxAPIWriteBytes) + `"}`, 413},
		{"/api/assets/1", "application/json", `{"revision":1,"location":"X"}` + strings.Repeat(" ", maxAPIWriteBytes), 413},
	}
	sessions := len(app.sessions)
	b.cookie = nil
	for _, tc := range cases {
		response := b.request("PATCH", tc.path, tc.contentType, strings.NewReader(tc.body))
		expect(t, response, tc.status)
		var failure apiErrorResponse
		readAPI(t, response, &failure)
		if failure.Error.Code == "" {
			t.Fatal("missing error code")
		}
		current, _ := app.store.Get(1)
		if current.Revision != 1 || current.Description != initial.Description || current.Location != initial.Location || string(current.Intake) != string(initial.Intake) || current.Photos[1] != initial.Photos[1] {
			t.Fatal("invalid PATCH changed record", tc.path)
		}
	}
	if len(app.sessions) != sessions {
		t.Fatal("failed writes created sessions")
	}
	if err := app.store.Archive(1, 1, true); err != nil {
		t.Fatal(err)
	}
	response := b.request("PATCH", "/api/assets/1", "application/json", strings.NewReader(`{"revision":2,"description":"Archived edit"}`))
	expect(t, response, 409)
	current, _ := app.store.Get(1)
	if current.Revision != 2 || !current.Archived || current.Description != "Original" {
		t.Fatal("archived entry mutated")
	}
}

func TestAPIWriteAuthorizationAndConcurrency(t *testing.T) {
	app, b, _ := start(t)
	image := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Concurrent"}}, image), 303)
	calls := 0
	app.apiWritePolicy = func(w http.ResponseWriter, r *http.Request) bool {
		calls++
		apiError(w, 401, "unauthorized", "Credentials required.")
		return false
	}
	for _, path := range []string{"/api/assets/1", "/api/assets/1/photos/missing"} {
		response := b.request("PATCH", path, "application/json", strings.NewReader(`null`))
		expect(t, response, 401)
	}
	current, _ := app.store.Get(1)
	if calls != 2 || current.Revision != 1 {
		t.Fatal("authorization bypassed")
	}
	expect(t, b.get("/api/assets/1"), 200)
	if calls != 2 {
		t.Fatal("read route incorrectly required write authorization")
	}
	app.apiWritePolicy = trustedAPIWrite
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, location := range []string{"Shelf A", "Shelf B"} {
		wg.Add(1)
		go func(location string) {
			defer wg.Done()
			client := &browser{app: app}
			response := client.request("PATCH", "/api/assets/1", "application/json", strings.NewReader(fmt.Sprintf(`{"revision":1,"location":%q}`, location)))
			statuses <- response.Code
		}(location)
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("concurrent writes lost revision protection", counts)
	}
}
