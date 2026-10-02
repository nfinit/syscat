package catalog

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func idHistoryCount(t *testing.T, s *Store) int {
	t.Helper()
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM asset_id_changes").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestIDHistoryTracksMovesSwapsAndOrigins(t *testing.T) {
	app, b, dir := numberFixture(t)
	var key string
	if err := app.store.db.QueryRow("SELECT submission_key FROM assets WHERE id=1").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.store.Renumber(3, 1, 10, 0, 0); err != nil {
		t.Fatal(err)
	}
	expect(t, renumberAPI(b, "/api/assets/1/id", `{"revision":1,"id":10,"swap_id":10,"swap_revision":2,"acknowledge_link_changes":true}`), 200)
	// The same source is now at 10. A browser move records the same private key.
	expect(t, b.post("/assets/10/id", url.Values{"revision": {"2"}, "catalog_number": {"20"}, "confirm": {"1"}, "acknowledge_link_changes": {"1"}}), 303)
	if idHistoryCount(t, app.store) != 3 {
		t.Fatal("missing/duplicate events")
	}
	rows, err := app.store.db.Query(`SELECT event_id,occurred_at,origin,old_id,new_id,source_title,source_revision,source_new_revision,swapped_title,swapped_revision,swapped_new_revision FROM asset_id_changes ORDER BY event_id`)
	if err != nil {
		t.Fatal(err)
	}
	type event struct {
		id                      int
		at, origin              string
		old, new                int
		title                   string
		before, after           int
		other                   sql.NullString
		otherBefore, otherAfter sql.NullInt64
	}
	var events []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.id, &e.at, &e.origin, &e.old, &e.new, &e.title, &e.before, &e.after, &e.other, &e.otherBefore, &e.otherAfter); err != nil {
			t.Fatal(err)
		}
		if _, err := time.Parse(time.RFC3339Nano, e.at); err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	rows.Close()
	if len(events) != 3 || events[0].origin != "internal" || events[0].old != 3 || events[0].new != 10 || events[0].other.Valid || events[1].origin != "api" || events[1].old != 1 || events[1].new != 10 || events[1].title != "First system" || events[1].before != 1 || events[1].after != 2 || events[1].other.String != "Prominent system" || events[1].otherBefore.Int64 != 2 || events[1].otherAfter.Int64 != 3 || events[2].origin != "browser" || events[2].old != 10 || events[2].new != 20 || events[2].other.Valid {
		t.Fatal(events)
	}
	var count int
	app.store.db.QueryRow("SELECT count(*) FROM asset_id_changes WHERE source_key=?", key).Scan(&count)
	if count != 2 {
		t.Fatal("identity lost across swaps", count)
	}
	var currentID int
	app.store.db.QueryRow(`SELECT a.id FROM assets a JOIN asset_id_changes h ON h.source_key=a.submission_key WHERE h.event_id=2`).Scan(&currentID)
	if currentID != 20 {
		t.Fatal("history cannot find current record", currentID)
	}
	// Later text edits do not rewrite history titles or intake references.
	asset, _ := app.store.Get(20)
	asset.ShortDescription = "Changed title"
	if err := app.store.Update(asset); err != nil {
		t.Fatal(err)
	}
	var title string
	app.store.db.QueryRow("SELECT source_title FROM asset_id_changes WHERE event_id=3").Scan(&title)
	if title != "First system" {
		t.Fatal(title)
	}
	app.store.Close()
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if idHistoryCount(t, reopened) != 3 {
		t.Fatal("history lost on restart")
	}
}

func TestIDHistoryRollsBackWithRenumberAndSkipsNoOps(t *testing.T) {
	app, b, _ := numberFixture(t)
	before, _ := app.store.Get(3)
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":1,"acknowledge_link_changes":true}`), 409)
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":3,"acknowledge_link_changes":true}`), 200)
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":100}`), 400)
	if idHistoryCount(t, app.store) != 0 {
		t.Fatal("failed or no-op change logged")
	}
	if _, err := app.store.db.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON asset_id_changes BEGIN SELECT RAISE(ABORT,'test history failure'); END`); err != nil {
		t.Fatal(err)
	}
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":100,"acknowledge_link_changes":true}`), 500)
	after, _ := app.store.Get(3)
	if !reflect.DeepEqual(before, after) || idHistoryCount(t, app.store) != 0 {
		t.Fatal("move committed without history")
	}
	var sequence int
	app.store.db.QueryRow("SELECT seq FROM sqlite_sequence WHERE name='assets'").Scan(&sequence)
	if sequence != 3 {
		t.Fatal("failed move advanced allocation", sequence)
	}
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":1,"swap_id":1,"swap_revision":1,"acknowledge_link_changes":true}`), 500)
	first, _ := app.store.Get(1)
	if first.ShortDescription != "First system" || first.Revision != 1 {
		t.Fatal("swap committed without history")
	}
	if _, err := app.store.db.Exec("DROP TRIGGER fail_history"); err != nil {
		t.Fatal(err)
	}
	expect(t, renumberAPI(b, "/api/assets/3/id", `{"revision":1,"id":100,"acknowledge_link_changes":true}`), 200)
	if idHistoryCount(t, app.store) != 1 {
		t.Fatal("successful move not logged once")
	}
}

func TestIDHistoryMigrationPreservesExistingRecords(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "syscat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"001.sql", "002.sql", "003.sql", "004.sql"} {
		script, err := migrations.ReadFile("migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	intake := `{"id":0,"description":"Original"}`
	if _, err = db.Exec(`INSERT INTO assets(id,short_description,details,intake,created_at,updated_at,revision,submission_key) VALUES(9,'Current title','Exact details',?,'created','updated',7,'private-key'); PRAGMA user_version=4`, intake); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c, err := store.Get(9)
	if err != nil || c.ShortDescription != "Current title" || c.Details != "Exact details" || string(c.Intake) != intake || c.Revision != 7 || c.CreatedAt != "created" || c.UpdatedAt != "updated" {
		t.Fatal(c, err)
	}
	if idHistoryCount(t, store) != 0 {
		t.Fatal("migration invented prior history")
	}
	var version int
	store.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 6 {
		t.Fatal(version)
	}
}
