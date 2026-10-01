package catalog

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCatalogNumberMigrationPreservesInventory(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "syscat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	script, err := migrations.ReadFile("migrations/001.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	intake := `{"id":0,"description":"Original observations","photos":[],"revision":1}`
	for _, id := range []int64{1, 9} {
		if _, err := db.Exec(`INSERT INTO assets(id,description,location,photos,intake,created_at,updated_at,revision,archived,submission_key) VALUES(?,?,?,'[]',?,'created','updated',7,?,?)`, id, fmt.Sprintf("Record %d", id), "Office", intake, id == 9, fmt.Sprint(id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 9} {
		asset, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if asset.ID != id || asset.CatalogNumber != id || asset.Revision != 7 || string(asset.Intake) != intake || asset.Location != "Office" || asset.Archived != (id == 9) || asset.CreatedAt != "created" || asset.UpdatedAt != "updated" {
			t.Fatal("migration rewrote inventory", asset)
		}
	}
	var version int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatal(version, err)
	}
	id, err := store.Create(Asset{Description: "New intake"}, randomKey())
	if err != nil || id != 10 {
		t.Fatal("sequence changed", id, err)
	}
	asset, _ := store.Get(id)
	if asset.CatalogNumber != 10 {
		t.Fatal(asset)
	}
	store.Close()
	store, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	asset, _ = store.Get(9)
	if asset.CatalogNumber != 9 || string(asset.Intake) != intake {
		t.Fatal("reopen changed migration")
	}
}

func numberFixture(t *testing.T) (*App, *browser, string) {
	t.Helper()
	app, b, dir := start(t)
	photo := pngPhoto(t)
	for _, name := range []string{"First system", "Second system", "Prominent system"} {
		expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {name}, "location": {"Office"}}, photo), 303)
	}
	return app, b, dir
}

func TestAPICatalogSwapSearchAndStableLinks(t *testing.T) {
	app, b, dir := numberFixture(t)
	before1, _ := app.store.Get(1)
	before3, _ := app.store.Get(3)
	files := photoFiles(t, dir)
	var old apiAsset
	readAPI(t, b.get("/api/assets/3"), &old)
	response := b.request("POST", "/api/assets/3/catalog-number", "application/json", strings.NewReader(`{"revision":1,"catalog_number":1,"swap_id":1,"swap_revision":1}`))
	expect(t, response, 200)
	var result apiNumberResult
	readAPI(t, response, &result)
	if result.Asset.ID != 3 || result.Asset.CatalogNumber != 1 || result.Asset.Label != "00001" || result.Asset.Revision != 2 || result.SwappedAsset == nil || result.SwappedAsset.ID != 1 || result.SwappedAsset.CatalogNumber != 3 || result.SwappedAsset.Revision != 2 {
		t.Fatal(result)
	}
	if result.Asset.APIURL != old.APIURL || result.Asset.URL != old.URL || result.Asset.PhotoUploadURL != old.PhotoUploadURL || !reflect.DeepEqual(result.Asset.Photos, old.Photos) || string(result.Asset.Intake) != string(old.Intake) {
		t.Fatal("swap changed identity or photos/intake")
	}
	for _, before := range []Asset{before1, before3} {
		after, err := app.store.Get(before.ID)
		if err != nil {
			t.Fatal(err)
		}
		expected := before
		expected.CatalogNumber = after.CatalogNumber
		expected.Revision++
		expected.UpdatedAt = after.UpdatedAt
		if !reflect.DeepEqual(expected, after) {
			t.Fatal("swap modified observations", after)
		}
	}
	if !reflect.DeepEqual(files, photoFiles(t, dir)) {
		t.Fatal("swap changed files")
	}
	expect(t, b.get(old.URL), 200)
	for _, view := range []string{"full", "summary"} {
		var listing struct {
			Assets []struct {
				ID, CatalogNumber int64
				Label             string
			}
			Total int
		}
		// Use raw wire names for the underscore-delimited field.
		var raw struct {
			Assets []struct {
				ID     int64 `json:"id"`
				Number int64 `json:"catalog_number"`
				Label  string
			}
			Total int
		}
		readAPI(t, b.get("/api/assets?view="+view), &raw)
		if len(raw.Assets) != 3 || raw.Assets[0].ID != 1 || raw.Assets[0].Number != 3 || raw.Assets[2].ID != 3 || raw.Assets[2].Label != "00001" {
			t.Fatal("catalog ordering/summary incorrect", raw)
		}
		for _, tc := range []struct {
			field string
			id    int64
		}{{"catalog_number", 3}, {"id", 1}} {
			readAPI(t, b.get("/api/assets?view="+view+"&field="+tc.field+"&q=00001"), &listing)
			if listing.Total != 1 || listing.Assets[0].ID != tc.id {
				t.Fatal("number/ID search conflated", listing)
			}
		}
	}
	expect(t, b.request("PATCH", old.APIURL, "application/json", strings.NewReader(`{"revision":1,"location":"Stale"}`)), 409)
	expect(t, b.request("PATCH", old.APIURL, "application/json", strings.NewReader(`{"revision":2,"location":"Shelf"}`)), 200)
	current, _ := app.store.Get(3)
	if current.CatalogNumber != 1 {
		t.Fatal("ordinary edit reverted numbering")
	}
	var exported []Asset
	readAPI(t, b.get("/export/assets.json"), &exported)
	if exported[0].ID != 1 || exported[0].CatalogNumber != 3 || exported[2].ID != 3 || exported[2].CatalogNumber != 1 {
		t.Fatal("JSON export lost identity/number")
	}
	rows, err := csv.NewReader(b.get("/export/assets.csv").Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "id" || rows[0][8] != "catalog_number" || rows[1][0] != "1" || rows[1][1] != "00003" || rows[1][8] != "3" {
		t.Fatal("CSV identity/number", rows)
	}
}

func TestCatalogRenumberGuardsRollbackAndAllocation(t *testing.T) {
	app, b, _ := numberFixture(t)
	for _, body := range []string{
		`{"revision":1,"catalog_number":1}`,
		`{"revision":1,"catalog_number":1,"swap_id":2,"swap_revision":1}`,
		`{"revision":1,"catalog_number":1,"swap_id":1,"swap_revision":2}`,
		`{"revision":2,"catalog_number":1,"swap_id":1,"swap_revision":1}`,
		`{"revision":1,"catalog_number":4,"swap_id":1,"swap_revision":1}`,
	} {
		expect(t, b.request("POST", "/api/assets/3/catalog-number", "application/json", strings.NewReader(body)), 409)
	}
	for _, body := range []string{
		`{"revision":1,"catalog_number":0}`,
		`{"revision":1,"catalog_number":-1}`,
		`{"revision":1,"catalog_number":1.5}`,
		`{"revision":1,"catalog_number":9223372036854775808}`,
		`{"revision":1,"catalog_number":1,"swap_id":1}`,
		`{"revision":1,"catalog_number":1,"swap_revision":1}`,
		`{"revision":1,"catalog_number":1,"swap_id":0,"swap_revision":1}`,
		`{"revision":1,"catalog_number":1,"swap_id":1,"swap_revision":0}`,
		`{"revision":1,"catalog_number":null}`,
		`{"revision":1,"catalog_number":1,"catalog_number":2}`,
		`{"revision":1,"catalog_number":4,"id":4}`,
	} {
		expect(t, b.request("POST", "/api/assets/3/catalog-number", "application/json", strings.NewReader(body)), 400)
	}
	before1, _ := app.store.Get(1)
	before3, _ := app.store.Get(3)
	if _, err := app.store.db.Exec(`CREATE TRIGGER fail_number BEFORE UPDATE OF catalog_number ON assets WHEN NEW.catalog_number=1 AND NEW.id=3 BEGIN SELECT RAISE(ABORT,'test swap failure'); END`); err != nil {
		t.Fatal(err)
	}
	expect(t, b.request("POST", "/api/assets/3/catalog-number", "application/json", strings.NewReader(`{"revision":1,"catalog_number":1,"swap_id":1,"swap_revision":1}`)), 500)
	after1, _ := app.store.Get(1)
	after3, _ := app.store.Get(3)
	if !reflect.DeepEqual(before1, after1) || !reflect.DeepEqual(before3, after3) {
		t.Fatal("partial swap committed")
	}
	if _, err := app.store.db.Exec("DROP TRIGGER fail_number"); err != nil {
		t.Fatal(err)
	}
	expect(t, b.request("POST", "/api/assets/3/catalog-number", "application/json", strings.NewReader(`{"revision":1,"catalog_number":3}`)), 200)
	after3, _ = app.store.Get(3)
	if !reflect.DeepEqual(before3, after3) {
		t.Fatal("no-op advanced revision")
	}
	// Archived numbers remain occupied and can be exchanged explicitly.
	if err := app.store.Archive(1, 1, true); err != nil {
		t.Fatal(err)
	}
	expect(t, b.request("POST", "/api/assets/1/catalog-number", "application/json", strings.NewReader(`{"revision":2,"catalog_number":4}`)), 409)
	expect(t, b.request("POST", "/api/assets/3/catalog-number", "application/json", strings.NewReader(`{"revision":1,"catalog_number":1,"swap_id":1,"swap_revision":2}`)), 200)
	after1, _ = app.store.Get(1)
	if !after1.Archived || after1.CatalogNumber != 3 || after1.Revision != 3 {
		t.Fatal(after1)
	}
	// Reserve the next sequential number, then create a new record without collision.
	expect(t, b.request("POST", "/api/assets/2/catalog-number", "application/json", strings.NewReader(`{"revision":1,"catalog_number":4}`)), 200)
	id, err := app.store.Create(Asset{Description: "Fourth"}, randomKey())
	if err != nil || id != 4 {
		t.Fatal(id, err)
	}
	fourth, _ := app.store.Get(4)
	if fourth.CatalogNumber != 5 {
		t.Fatal("allocation collided with reserved number", fourth)
	}
	var intake Asset
	if err := json.Unmarshal(fourth.Intake, &intake); err != nil || intake.CatalogNumber != 5 {
		t.Fatal("initial number missing from new intake", intake, err)
	}
}

func TestNumberEditorPreviewAndConcurrentChanges(t *testing.T) {
	app, b, _ := numberFixture(t)
	editor := b.get("/assets/3/catalog-number")
	expect(t, editor, 200)
	if !strings.Contains(editor.Body.String(), "Preview change") {
		t.Fatal("number editor not rendered")
	}
	preview := b.post("/assets/3/catalog-number", url.Values{"revision": {"1"}, "catalog_number": {"1"}})
	expect(t, preview, 200)
	if !strings.Contains(preview.Body.String(), "First system") || !strings.Contains(preview.Body.String(), "Swap numbers") || hidden(preview.Body.String(), "swap_id") != "1" || hidden(preview.Body.String(), "swap_revision") != "1" {
		t.Fatal("swap preview does not identify both records")
	}
	before, _ := app.store.Get(3)
	if before.CatalogNumber != 3 {
		t.Fatal("preview performed swap")
	}
	expect(t, b.post("/assets/1", url.Values{"revision": {"1"}, "description": {"First system edited"}}), 303)
	confirm := url.Values{"revision": {"1"}, "catalog_number": {"1"}, "confirm": {"1"}, "swap_id": {"1"}, "swap_revision": {"1"}}
	expect(t, b.post("/assets/3/catalog-number", confirm), 409)
	confirm.Set("swap_revision", "2")
	response := b.post("/assets/3/catalog-number", confirm)
	expect(t, response, 303)
	if response.Header().Get("Location") != "/assets/3?updated=1" {
		t.Fatal("renumber changed URL")
	}
	current, _ := app.store.Get(3)
	if current.CatalogNumber != 1 || current.Revision != 2 {
		t.Fatal(current)
	}
	expect(t, b.post("/assets/3", url.Values{"revision": {"1"}, "description": {"Stale form"}}), 409)
	expect(t, b.post("/assets/3/catalog-number", url.Values{"csrf": {"wrong"}, "revision": {"2"}, "catalog_number": {"10"}}), 400)
	expect(t, b.post("/assets/3/catalog-number", url.Values{"revision": {"2"}, "catalog_number": {"0"}}), 400)
	// Free-number preview includes no swap expectations; a new occupant makes its
	// confirmation stale rather than being silently displaced.
	expect(t, b.post("/assets/3/catalog-number", url.Values{"revision": {"2"}, "catalog_number": {"10"}}), 200)
	if _, _, err := app.store.Renumber(2, 1, 10, 0, 0); err != nil {
		t.Fatal(err)
	}
	expect(t, b.post("/assets/3/catalog-number", url.Values{"revision": {"2"}, "catalog_number": {"10"}, "confirm": {"1"}}), 409)
	app.apiWritePolicy = func(w http.ResponseWriter, r *http.Request) bool {
		apiError(w, 403, "forbidden", "test policy")
		return false
	}
	expect(t, b.request("POST", "/api/assets/3/catalog-number", "", nil), 403)
	app.apiWritePolicy = trustedAPIWrite
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, number := range []int{11, 12} {
		wg.Add(1)
		go func(number int) {
			defer wg.Done()
			client := &browser{app: app}
			result := client.request("POST", "/api/assets/3/catalog-number", "application/json", strings.NewReader(fmt.Sprintf(`{"revision":2,"catalog_number":%d}`, number)))
			statuses <- result.Code
		}(number)
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("concurrent renumber lost revision protection", counts)
	}
}
