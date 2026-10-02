package catalog

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestDescriptionSplitMigrationPreservesLegacyRecords(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "syscat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"001.sql", "002.sql"} {
		script, err := migrations.ReadFile("migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	originals := []string{"Single line", "Unicode café\r\n\r\nIndented:\r\n  first line\r\n  second line\r\n", "Title\n\nParagraph\n\nFinal paragraph", "", "\nBody without title", "Trailing newline\n"}
	intake := `{"id":0,"description":"Original observations","photos":[],"revision":1}`
	for i, description := range originals {
		id := i + 1
		if _, err := db.Exec(`INSERT INTO assets(id,catalog_number,description,location,photos,intake,created_at,updated_at,revision,archived,submission_key) VALUES(?,?,?,'Office','[]',?,'created','updated',9,?,?)`, id, id+100, description, intake, i%2, fmt.Sprint(id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i, description := range originals {
		asset, err := store.Get(int64(i + 101))
		if err != nil {
			t.Fatal(err)
		}
		short, details := splitDescription(description)
		if asset.Description != description || asset.ShortDescription != short || asset.Details != details || string(asset.Intake) != intake || asset.CatalogNumber != int64(i+101) || asset.Revision != 9 || asset.CreatedAt != "created" || asset.UpdatedAt != "updated" || asset.Archived != (i%2 == 1) {
			t.Fatal("migration rewrote legacy data", asset)
		}
	}
	var version int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
		t.Fatal(version, err)
	}
	// A non-text edit must preserve the exact legacy combined representation too.
	asset, _ := store.Get(103)
	asset.Location = "Shelf"
	if err := store.Update(asset); err != nil {
		t.Fatal(err)
	}
	after, _ := store.Get(103)
	if after.Description != originals[2] || after.Details != asset.Details || string(after.Intake) != intake {
		t.Fatal("ordinary edit rewrote notes")
	}
	store.Close()
	store, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	asset, _ = store.Get(102)
	if asset.Details != "\r\nIndented:\r\n  first line\r\n  second line\r\n" {
		t.Fatal("migration was not idempotent", asset)
	}
}

func TestIndependentDescriptionAPIUpdates(t *testing.T) {
	app, b, _ := start(t)
	details := "\nDetailed research:\r\n  line one\r\n\r\n  line two\n"
	if _, err := app.store.Create(Asset{ShortDescription: "Initial title", Details: details, Location: "Office", Photos: []Photo{{}}}, randomKey()); err != nil {
		t.Fatal(err)
	}
	initial, _ := app.store.Get(1)
	patch := func(body string, status int) apiAsset {
		t.Helper()
		response := b.request("PATCH", "/api/assets/1", "application/json", strings.NewReader(body))
		expect(t, response, status)
		var result apiAsset
		if status == 200 {
			readAPI(t, response, &result)
		}
		return result
	}
	updated := patch(`{"revision":1,"short_description":"Researched title"}`, 200)
	if updated.Title != "Researched title" || updated.ShortDescription != "Researched title" || updated.Details != details || updated.Description != combinedDescription(updated.ShortDescription, details) || string(updated.Intake) != string(initial.Intake) || updated.Revision != 2 {
		t.Fatal("title edit changed detailed notes", updated)
	}
	updated = patch(`{"revision":2,"details":"New detailed observations\nAnother paragraph"}`, 200)
	if updated.ShortDescription != "Researched title" || updated.Details != "New detailed observations\nAnother paragraph" || updated.Revision != 3 {
		t.Fatal(updated)
	}
	updated = patch(`{"revision":3,"details":""}`, 200)
	if updated.Description != "Researched title" || updated.Details != "" || updated.Revision != 4 {
		t.Fatal(updated)
	}
	for _, body := range []string{
		`{"revision":4,"short_description":""}`,
		`{"revision":4,"short_description":"line one\nline two"}`,
		`{"revision":4,"short_description":"line one\rline two"}`,
		`{"revision":4,"short_description":null}`,
		`{"revision":4,"details":42}`,
		`{"revision":4,"description":"Legacy","details":"Mixed"}`,
		`{"revision":4,"description":"Legacy","short_description":"Mixed"}`,
	} {
		patch(body, 400)
	}
	patch(`{"revision":3,"short_description":"Stale"}`, 409)
	for _, field := range []string{"short_description", "details"} {
		payload, _ := json.Marshal(map[string]any{"revision": 4, field: strings.Repeat("x", 20001)})
		patch(string(payload), 400)
	}
	after, _ := app.store.Get(1)
	if after.Revision != 4 || after.ShortDescription != "Researched title" || after.Details != "" {
		t.Fatal("invalid patch changed data", after)
	}
	// Older clients still get and can write the combined field explicitly.
	updated = patch(`{"revision":4,"description":"Legacy title\nLegacy details"}`, 200)
	if updated.ShortDescription != "Legacy title" || updated.Details != "Legacy details" || updated.Revision != 5 || string(updated.Intake) != string(initial.Intake) {
		t.Fatal("legacy bridge failed", updated)
	}
	for _, test := range []struct {
		field, q string
		total    int
	}{{"title", "Legacy title", 1}, {"short_description", "Legacy title", 1}, {"title", "Legacy details", 0}, {"short_description", "Legacy details", 0}, {"details", "Legacy title", 0}, {"details", "Legacy details", 1}, {"description", "Legacy title", 1}, {"description", "Legacy details", 1}} {
		for _, view := range []string{"full", "summary"} {
			var result struct{ Total int }
			readAPI(t, b.get("/api/assets?"+url.Values{"view": {view}, "field": {test.field}, "q": {test.q}}.Encode()), &result)
			if result.Total != test.total {
				t.Fatal("split search wrong", test, view, result)
			}
		}
	}
}

func TestSplitIntakeFormsAPIAndExports(t *testing.T) {
	app, b, _ := start(t)
	photo := pngPhoto(t)
	fields := url.Values{"short_description": {" Separate title "}, "details": {" Detailed observations\nMore detail "}, "location": {"Office"}}
	response := apiCreate(t, b, "split-intake", fields, map[string][][]byte{"overview": {photo}})
	expect(t, response, 201)
	var created apiAsset
	readAPI(t, response, &created)
	if created.ShortDescription != "Separate title" || created.Title != created.ShortDescription || created.Details != " Detailed observations\nMore detail " {
		t.Fatal(created)
	}
	var intake Asset
	if err := json.Unmarshal(created.Intake, &intake); err != nil || intake.ShortDescription != created.ShortDescription || intake.Details != created.Details {
		t.Fatal("split intake missing", intake, err)
	}
	page := b.get("/assets/1")
	expect(t, page, 200)
	for _, text := range []string{`name="short_description"`, `name="details"`, `value="Separate title"`, `Detailed observations`, `Change ID`} {
		if !strings.Contains(page.Body.String(), text) {
			t.Fatal("split edit form missing", text)
		}
	}
	if strings.Contains(page.Body.String(), `name="description"`) {
		t.Fatal("edit form still submits combined description")
	}
	expect(t, b.post("/assets/1", url.Values{"revision": {"1"}, "short_description": {"Edited title"}, "details": {"Edited details"}, "location": {"Office"}}), 303)
	saved, _ := app.store.Get(1)
	if saved.ShortDescription != "Edited title" || saved.Details != "Edited details" || string(saved.Intake) != string(created.Intake) {
		t.Fatal(saved)
	}
	values := url.Values{"submission": {randomKey()}, "short_description": {"Minimal title"}}
	expect(t, b.upload("/assets", values, photo), 303)
	minimal, _ := app.store.Get(2)
	if minimal.Details != "" || minimal.ShortDescription != "Minimal title" {
		t.Fatal(minimal)
	}
	for _, values := range []url.Values{
		{"short_description": {""}, "details": {"Body without title"}},
		{"short_description": {"One", "Two"}},
		{"short_description": {"Title"}, "description": {"Mixed legacy"}},
		{"short_description": {"Multi\nline"}},
	} {
		expect(t, apiCreate(t, b, "invalid-split", values, map[string][][]byte{"overview": {photo}}), 400)
	}
	expect(t, b.post("/assets/1", url.Values{"revision": {"2"}, "short_description": {""}, "details": {"Retained notes"}}), 400)
	expect(t, b.post("/assets/1", url.Values{"revision": {"1"}, "short_description": {"Stale title"}, "details": {"Stale details"}}), 409)
	var exported []Asset
	readAPI(t, b.get("/export/assets.json"), &exported)
	if len(exported) != 2 || exported[1].ShortDescription != "Edited title" || exported[1].Details != "Edited details" {
		t.Fatal("split export fields missing", exported)
	}
	rows, err := csv.NewReader(b.get("/export/assets.csv").Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][9] != "short_description" || rows[0][10] != "details" || rows[2][9] != "Edited title" || rows[2][10] != "Edited details" {
		t.Fatal(rows)
	}
	if err := app.store.Archive(1, 2, true); err != nil {
		t.Fatal(err)
	}
	page = b.get("/assets/1")
	expect(t, page, 200)
	if !strings.Contains(page.Body.String(), "Edited details") || !strings.Contains(page.Body.String(), "Detailed observations") {
		t.Fatal("archived/current intake details lost")
	}
}

func TestSplitFormTitleEditPreservesDetailedFormatting(t *testing.T) {
	app, b, _ := start(t)
	details := "\nResearch notes:\n\n    indented line\nTrailing paragraph\n"
	response := apiCreate(t, b, "formatting", url.Values{"short_description": {"Initial title"}, "details": {details}}, map[string][][]byte{"overview": {pngPhoto(t)}})
	expect(t, response, 201)
	before, _ := app.store.Get(1)
	// A browser serializes textarea line endings as CRLF, even when the stored
	// notes originated from an agent using LF. Unchanged notes remain byte exact.
	expect(t, b.post("/assets/1", url.Values{"revision": {"1"}, "short_description": {"Changed title"}, "details": {strings.ReplaceAll(details, "\n", "\r\n")}}), 303)
	after, _ := app.store.Get(1)
	if after.Details != details || after.ShortDescription != "Changed title" || string(after.Intake) != string(before.Intake) {
		t.Fatal("title-only form edit changed notes", after)
	}
}
