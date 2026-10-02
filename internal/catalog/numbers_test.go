package catalog

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSingleIDMigrationPreservesInventory(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "syscat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"001.sql", "002.sql", "003.sql"} {
		script, err := migrations.ReadFile("migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	intake := `{"id":0,"catalog_number":9,"description":"Original observations","photos":[],"revision":1}`
	for _, pair := range [][2]int64{{1, 3}, {3, 1}, {9, 100}} {
		_, err = db.Exec(`INSERT INTO assets(id,catalog_number,description,short_description,details,location,photos,intake,created_at,updated_at,revision,archived,submission_key) VALUES(?,?,?,?,'Exact details','Office','[]',?,'created','updated',7,?,?)`, pair[0], pair[1], fmt.Sprintf("Record %d\nExact details", pair[0]), fmt.Sprintf("Record %d", pair[0]), intake, pair[0] == 9, fmt.Sprint(pair[0]))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("PRAGMA user_version=3; UPDATE sqlite_sequence SET seq=50 WHERE name='assets'"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]int64{{1, 3}, {3, 1}, {9, 100}} {
		c, err := store.Get(pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if c.ID != pair[1] || c.CatalogNumber != c.ID || c.ShortDescription != fmt.Sprintf("Record %d", pair[0]) || c.Details != "Exact details" || c.Revision != 7 || c.Location != "Office" || c.Archived != (pair[0] == 9) || c.CreatedAt != "created" || c.UpdatedAt != "updated" || string(c.Intake) != intake {
			t.Fatal("migration lost observations", c)
		}
	}
	var version int
	store.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 6 {
		t.Fatal(version)
	}
	rows, err := store.db.Query("PRAGMA table_info(assets)")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n, required, primary int
		var name, kind string
		var value any
		rows.Scan(&n, &name, &kind, &required, &value, &primary)
		if name == "catalog_number" {
			t.Fatal("separate number column remains")
		}
	}
	rows.Close()
	id, err := store.Create(Asset{Description: "New intake"}, randomKey())
	if err != nil || id != 10 {
		t.Fatal("sequence collision", id, err)
	}
	store.Close()
	store, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c, _ := store.Get(1)
	if c.ShortDescription != "Record 3" || string(c.Intake) != intake {
		t.Fatal("migration repeated", c)
	}
	// Moving an ID never changes the intake counter.
	if _, _, err = store.Renumber(10, 1, 50, 0, 0); err != nil {
		t.Fatal(err)
	}
	id, err = store.Create(Asset{Description: "Next intake"}, randomKey())
	if err != nil || id != 11 {
		t.Fatal(id, err)
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
func renumberAPI(b *browser, path, body string) *httptest.ResponseRecorder {
	return b.request("POST", path, "application/json", strings.NewReader(body))
}

func TestAPISingleIDSwapAndBreakingLinks(t *testing.T) {
	app, b, dir := numberFixture(t)
	first, _ := app.store.Get(1)
	source, _ := app.store.Get(3)
	files := photoFiles(t, dir)
	var old apiAsset
	readAPI(t, b.get("/api/assets/3"), &old)
	response := renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":1,"swap_id":1,"swap_revision":1,"acknowledge_link_changes":true}`)
	expect(t, response, 200)
	var result apiNumberResult
	readAPI(t, response, &result)
	if result.Asset.ID != 1 || result.Asset.CatalogNumber != 1 || result.Asset.Revision != 2 || result.Asset.URL != "/assets/1" || result.Asset.IDChangeURL != "/api/assets/1/id" || response.Header().Get("Location") != "/api/assets/1" || result.SwappedAsset == nil || result.SwappedAsset.ID != 3 || result.SwappedAsset.Revision != 2 {
		t.Fatal(result)
	}
	for i := range old.Photos {
		old.Photos[i].APIURL = result.Asset.Photos[i].APIURL
	}
	if !reflect.DeepEqual(result.Asset.Photos, old.Photos) || string(result.Asset.Intake) != string(old.Intake) {
		t.Fatal("photo/intake identity changed")
	}
	for _, before := range []Asset{first, source} {
		target := int64(3)
		if before.ID == 3 {
			target = 1
		}
		after, _ := app.store.Get(target)
		expected := before
		expected.ID = target
		expected.CatalogNumber = target
		expected.Revision++
		expected.UpdatedAt = after.UpdatedAt
		if !reflect.DeepEqual(expected, after) {
			t.Fatal("observations changed", after)
		}
	}
	var oldLink apiAsset
	readAPI(t, b.get(old.APIURL), &oldLink)
	if oldLink.ShortDescription != first.ShortDescription {
		t.Fatal("old link not reassigned")
	}
	if !reflect.DeepEqual(files, photoFiles(t, dir)) {
		t.Fatal("files changed")
	}
	for _, view := range []string{"full", "summary"} {
		for _, field := range []string{"id", "catalog_number"} {
			var list struct {
				Assets []apiAsset
				Total  int
			}
			readAPI(t, b.get("/api/assets?view="+view+"&field="+field+"&q=00001"), &list)
			if list.Total != 1 || list.Assets[0].ID != 1 || list.Assets[0].Title != source.Title() {
				t.Fatal(list)
			}
		}
	}
	rows, err := csv.NewReader(b.get("/export/assets.csv").Body).ReadAll()
	if err != nil || rows[1][0] != "3" || rows[1][1] != "00003" || rows[1][8] != "3" {
		t.Fatal(rows, err)
	}
	// A move vacates the old URL, and idempotent intake still finds the same asset.
	moved := renumberAPI(b, "/api/assets/1/catalog-number", `{"revision":2,"catalog_number":20,"acknowledge_link_changes":true}`)
	expect(t, moved, 200)
	expect(t, b.get("/assets/1"), 404)
	c, _ := app.store.Get(20)
	if c.ShortDescription != source.ShortDescription {
		t.Fatal(c)
	}
	var key string
	app.store.db.QueryRow("SELECT submission_key FROM assets WHERE id=20").Scan(&key)
	replay, err := app.store.BySubmission(key)
	if err != nil || replay.ID != 20 {
		t.Fatal(replay, err)
	}
	expect(t, b.request("PATCH", "/api/assets/20", "application/json", strings.NewReader(`{"revision":3,"details":"New research"}`)), 200)
	expect(t, b.request("GET", "/api/assets/20/id", "", nil), 405)
}

func TestSingleIDGuardsRollbackAndAllocation(t *testing.T) {
	app, b, _ := numberFixture(t)
	for _, body := range []string{`{"revision":1,"id":1}`, `{"revision":1,"id":1,"acknowledge_link_changes":false}`, `{"revision":1,"id":1,"acknowledge_link_changes":"true"}`, `{"revision":1,"id":1,"catalog_number":1,"acknowledge_link_changes":true}`, `{"revision":1,"id":0,"acknowledge_link_changes":true}`, `{"revision":1,"id":1,"swap_id":1,"acknowledge_link_changes":true}`, `{"revision":1,"id":9223372036854775808,"acknowledge_link_changes":true}`} {
		expect(t, renumberAPI(b, "/api/assets/3/id", body), 400)
	}
	for _, body := range []string{`{"revision":1,"id":1,"acknowledge_link_changes":true}`, `{"revision":1,"id":1,"swap_id":2,"swap_revision":1,"acknowledge_link_changes":true}`, `{"revision":1,"id":1,"swap_id":1,"swap_revision":2,"acknowledge_link_changes":true}`, `{"revision":2,"id":1,"swap_id":1,"swap_revision":1,"acknowledge_link_changes":true}`, `{"revision":1,"id":4,"swap_id":1,"swap_revision":1,"acknowledge_link_changes":true}`} {
		expect(t, renumberAPI(b, "/api/assets/3/id", body), 409)
	}
	first, _ := app.store.Get(1)
	source, _ := app.store.Get(3)
	_, err := app.store.db.Exec(`CREATE TRIGGER fail_number BEFORE UPDATE OF id ON assets WHEN NEW.id=1 AND OLD.id=-3 BEGIN SELECT RAISE(ABORT,'test swap failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":1,"swap_id":1,"swap_revision":1,"acknowledge_link_changes":true}`), 500)
	after1, _ := app.store.Get(1)
	after3, _ := app.store.Get(3)
	if !reflect.DeepEqual(first, after1) || !reflect.DeepEqual(source, after3) {
		t.Fatal("partial swap")
	}
	app.store.db.Exec("DROP TRIGGER fail_number")
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":3,"acknowledge_link_changes":true}`), 200)
	after3, _ = app.store.Get(3)
	if !reflect.DeepEqual(source, after3) {
		t.Fatal("no-op changed revision")
	}
	if err = app.store.Archive(1, 1, true); err != nil {
		t.Fatal(err)
	}
	expect(t, renumberAPI(b, "/api/assets/1/id", `{"revision":2,"id":4,"acknowledge_link_changes":true}`), 409)
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":1,"swap_id":1,"swap_revision":2,"acknowledge_link_changes":true}`), 200)
	archived, _ := app.store.Get(3)
	if !archived.Archived || archived.Revision != 3 {
		t.Fatal(archived)
	}
	expect(t, renumberAPI(b, "/api/assets/2/id", `{"revision":1,"id":100,"acknowledge_link_changes":true}`), 200)
	id, err := app.store.Create(Asset{Description: "Next"}, randomKey())
	if err != nil || id != 4 {
		t.Fatal(id, err)
	}
	c, _ := app.store.Get(id)
	var intake Asset
	if err = json.Unmarshal(c.Intake, &intake); err != nil || intake.CatalogNumber != 4 {
		t.Fatal(intake, err)
	}
}

func TestSingleIDBrowserAcknowledgmentAndConcurrentChanges(t *testing.T) {
	app, b, _ := numberFixture(t)
	preview := b.post("/assets/3/id", url.Values{"revision": {"1"}, "catalog_number": {"1"}})
	expect(t, preview, 200)
	for _, text := range []string{"First system", "Swap IDs", "Changing IDs breaks existing links", "required", "acknowledge_link_changes"} {
		if !strings.Contains(preview.Body.String(), text) {
			t.Fatal("preview missing", text)
		}
	}
	confirm := url.Values{"revision": {"1"}, "catalog_number": {"1"}, "confirm": {"1"}, "swap_id": {"1"}, "swap_revision": {"1"}}
	expect(t, b.post("/assets/3/id", confirm), 400)
	confirm.Set("acknowledge_link_changes", "1")
	expect(t, b.post("/assets/1", url.Values{"revision": {"1"}, "description": {"First system edited"}}), 303)
	expect(t, b.post("/assets/3/id", confirm), 409)
	confirm.Set("swap_revision", "2")
	response := b.post("/assets/3/id", confirm)
	expect(t, response, 303)
	if response.Header().Get("Location") != "/assets/1?updated=1" {
		t.Fatal(response.Header())
	}
	// A preview of a free target must not silently swap a new occupant.
	expect(t, b.post("/assets/1/id", url.Values{"revision": {"2"}, "catalog_number": {"10"}}), 200)
	if _, _, err := app.store.Renumber(2, 1, 10, 0, 0); err != nil {
		t.Fatal(err)
	}
	expect(t, b.post("/assets/1/id", url.Values{"revision": {"2"}, "catalog_number": {"10"}, "confirm": {"1"}, "acknowledge_link_changes": {"1"}}), 409)
	app.apiWritePolicy = func(w http.ResponseWriter, r *http.Request) bool {
		apiError(w, 403, "forbidden", "test policy")
		return false
	}
	expect(t, b.request("POST", "/api/assets/1/id", "", nil), 403)
	app.apiWritePolicy = trustedAPIWrite
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, id := range []int{11, 12} {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := &browser{app: app}
			response := renumberAPI(client, "/api/assets/1/id", fmt.Sprintf(`{"revision":2,"id":%d,"acknowledge_link_changes":true}`, id))
			statuses <- response.Code
		}(id)
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || counts[404]+counts[409] != 1 {
		t.Fatal("concurrent moves", counts)
	}
	var negative int
	app.store.db.QueryRow("SELECT count(*) FROM assets WHERE id<1").Scan(&negative)
	if negative != 0 {
		t.Fatal("temporary ID escaped transaction")
	}
}
