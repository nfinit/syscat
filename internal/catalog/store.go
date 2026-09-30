package catalog

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrConflict = errors.New("entry modified elsewhere; reopen the record before saving")

type Photo struct {
	Path      string `json:"path"`
	Thumbnail string `json:"thumbnail"`
	Name      string `json:"original_name"`
	Caption   string `json:"caption,omitempty"`
	Group     string `json:"group,omitempty"`
}

type Asset struct {
	ID          int64           `json:"id"`
	Description string          `json:"description"`
	Location    string          `json:"location"`
	Photos      []Photo         `json:"photos"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	Revision    int             `json:"revision"`
	Archived    bool            `json:"archived"`
	Intake      json.RawMessage `json:"original_intake,omitempty"`
}

func (c Asset) Label() string { return fmt.Sprintf("%05d", c.ID) }
func (c Asset) Title() string {
	if c.Description == "" {
		return "Unidentified item"
	}
	title := []rune(strings.SplitN(c.Description, "\n", 2)[0])
	if len(title) > 120 {
		return string(title[:120]) + "..."
	}
	return string(title)
}

type Store struct{ db *sql.DB }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "syscat.sqlite3"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.initialize(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) initialize() error {
	if _, err := s.db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("database schema %d is newer than this Syscat supports (1)", version)
	}
	if version == 0 {
		script, err := migrations.ReadFile("migrations/001.sql")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(script)); err != nil {
			return err
		}
		if _, err := tx.Exec("PRAGMA user_version=1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const columns = `id, description, location, photos, created_at, updated_at, revision, archived, intake`

type scanner interface{ Scan(...any) error }

func scanAsset(row scanner) (Asset, error) {
	var c Asset
	var photos, intake string
	err := row.Scan(&c.ID, &c.Description, &c.Location, &photos, &c.CreatedAt, &c.UpdatedAt, &c.Revision, &c.Archived, &intake)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(photos), &c.Photos); err != nil {
		return c, err
	}
	c.Intake = json.RawMessage(intake)
	return c, nil
}

func (s *Store) Get(id int64) (Asset, error) {
	return scanAsset(s.db.QueryRow("SELECT "+columns+" FROM assets WHERE id=?", id))
}

func (s *Store) BySubmission(key string) (Asset, error) {
	return scanAsset(s.db.QueryRow("SELECT "+columns+" FROM assets WHERE submission_key=?", key))
}

func (s *Store) Create(c Asset, key string) (int64, error) {
	if c.Photos == nil {
		c.Photos = []Photo{}
	}
	photos, err := json.Marshal(c.Photos)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	c.CreatedAt, c.UpdatedAt, c.Revision = now, now, 1
	// Keep the initial observations even when the current fields are edited.
	intake, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	result, err := s.db.Exec(`INSERT INTO assets(description, location, photos, intake, created_at, updated_at, submission_key) VALUES(?,?,?,?,?,?,?)`, c.Description, c.Location, string(photos), string(intake), now, now, key)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) Update(c Asset) error {
	photos, err := json.Marshal(c.Photos)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE assets SET description=?, location=?, photos=?, updated_at=?, revision=revision+1 WHERE id=? AND revision=? AND archived=0`, c.Description, c.Location, string(photos), time.Now().UTC().Format(time.RFC3339Nano), c.ID, c.Revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrConflict
	}
	return err
}

func (s *Store) Archive(id int64, revision int, archived bool) error {
	result, err := s.db.Exec(`UPDATE assets SET archived=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?`, archived, time.Now().UTC().Format(time.RFC3339Nano), id, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrConflict
	}
	return err
}

func (s *Store) List(query string, archived bool, limit, offset int) ([]Asset, int, error) {
	where := ` WHERE archived=?`
	args := []any{archived}
	if query != "" {
		where += ` AND ((description || ' ' || location) LIKE ? ESCAPE '\' OR id=?)`
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
		id, err := strconv.ParseInt(query, 10, 64)
		if err != nil || id < 1 {
			id = 0
		}
		args = append(args, "%"+escaped+"%", id)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM assets"+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query("SELECT "+columns+" FROM assets"+where+" ORDER BY id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	assets := []Asset{}
	for rows.Next() {
		c, err := scanAsset(rows)
		if err != nil {
			return nil, 0, err
		}
		assets = append(assets, c)
	}
	return assets, count, rows.Err()
}

func (s *Store) Suggestions(field string) ([]string, error) {
	switch field {
	case "location":
	default:
		return nil, errors.New("invalid suggestion field")
	}
	rows, err := s.db.Query("SELECT DISTINCT " + field + " FROM assets WHERE archived=0 AND " + field + "<>'' ORDER BY " + field + " COLLATE NOCASE LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
