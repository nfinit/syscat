package catalog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestIntakeAllocatorSkipsVanityAndArchivedIDs(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	for i := int64(1); i <= 3; i++ {
		id, err := s.Create(Asset{Description: "Original"}, fmt.Sprint(i))
		if err != nil || id != i {
			t.Fatal(id, err)
		}
	}
	for _, move := range [][2]int64{{1, math.MaxInt64}, {2, 6}, {3, 7}} {
		if _, _, err := s.Renumber(move[0], 1, move[1], 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Archive(6, 2, true); err != nil {
		t.Fatal(err)
	}
	// A failed intake must not burn the next number.
	if _, err := s.Create(Asset{Description: "Duplicate"}, "1"); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_allocator BEFORE UPDATE ON asset_id_allocator BEGIN SELECT RAISE(ABORT,'test allocation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Asset{Description: "Failed counter update"}, "failed-counter"); err == nil {
		t.Fatal("counter failure ignored")
	}
	if _, err := s.BySubmission("failed-counter"); err != sql.ErrNoRows {
		t.Fatal("failed allocation left an asset", err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_allocator"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{4, 5, 8} {
		id, err := s.Create(Asset{Description: "Next"}, randomKey())
		if err != nil || id != want {
			t.Fatal(id, want, err)
		}
		asset, _ := s.Get(id)
		var original Asset
		if err := json.Unmarshal(asset.Intake, &original); err != nil || original.ID != id || original.CatalogNumber != id {
			t.Fatal(original, err)
		}
	}
	// Vacating a previously allocated ID does not rewind intake.
	if _, _, err := s.Renumber(8, 1, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Create(Asset{Description: "After restart"}, randomKey())
	if err != nil || id != 9 {
		t.Fatal(id, err)
	}
}

func TestIntakeAllocatorMigrationRecoversCounter(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "syscat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		script, err := migrations.ReadFile(fmt.Sprintf("migrations/%03d.sql", i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		id          int64
		key, intake string
	}{
		{60, "z600", `{"id":0,"description":"Original Z600"}`},
		{43, "old", `{"id":0}`},
		{2, "swapped", `{"id":0}`},
		{500, "new", `{"id":0,"catalog_number":44}`},
	} {
		if _, err := db.Exec(`INSERT INTO assets(id,short_description,intake,created_at,updated_at,submission_key) VALUES(?,'Current',?,'created','updated',?)`, row.id, row.intake, row.key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO asset_id_changes(occurred_at,origin,old_id,new_id,source_key,source_title,source_revision,source_new_revision,swapped_key,swapped_title,swapped_revision,swapped_new_revision) VALUES
        ('time','browser',42,60,'z600','Z600',3,4,NULL,NULL,NULL,NULL),
        ('time','api',2,44,'new','New',1,2,'swapped','Swapped',1,2);
        UPDATE sqlite_sequence SET seq=500 WHERE name='assets'; PRAGMA user_version=5`); err != nil {
		t.Fatal(err)
	}
	before, err := db.Query(`SELECT id,intake FROM assets ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := map[int64]string{}
	for before.Next() {
		var id int64
		var intake string
		if err := before.Scan(&id, &intake); err != nil {
			t.Fatal(err)
		}
		snapshots[id] = intake
	}
	before.Close()
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var next int64
	if err := s.db.QueryRow("SELECT next_id FROM asset_id_allocator").Scan(&next); err != nil || next != 45 {
		t.Fatal(next, err)
	}
	for id, intake := range snapshots {
		asset, err := s.Get(id)
		if err != nil || string(asset.Intake) != intake || asset.Revision != 1 {
			t.Fatal(asset, err)
		}
	}
	if idHistoryCount(t, s) != 2 {
		t.Fatal("history changed")
	}
	id, err := s.Create(Asset{Description: "Next intake"}, randomKey())
	if err != nil || id != 45 {
		t.Fatal(id, err)
	}
}

func TestIntakeAllocatorConcurrentCreatesAndExhaustion(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	ids := make(chan int64, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.Create(Asset{Description: "Concurrent"}, randomKey())
			ids <- id
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := map[int64]bool{}
	want := map[int64]bool{}
	for id := range ids {
		got[id] = true
	}
	for id := int64(1); id <= 20; id++ {
		want[id] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if _, err := s.db.Exec("UPDATE asset_id_allocator SET next_id=?", int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	id, err := s.Create(Asset{Description: "Last possible ID"}, randomKey())
	if err != nil || id != math.MaxInt64 {
		t.Fatal(id, err)
	}
	if _, err := s.Create(Asset{Description: "Exhausted"}, randomKey()); err == nil {
		t.Fatal("exhausted sequence accepted intake")
	}
}
