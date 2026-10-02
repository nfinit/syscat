package catalog

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
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

// IDs derive from immutable file paths, so existing inventories need no migration.
// Never use the mutable array position to identify a photo.
func (p Photo) ID() string {
	digest := sha256.Sum256([]byte(p.Path))
	return hex.EncodeToString(digest[:16])
}

type Asset struct {
	ID               int64           `json:"id"`
	CatalogNumber    int64           `json:"catalog_number,omitempty"`
	Description      string          `json:"description"`
	ShortDescription string          `json:"short_description"`
	Details          string          `json:"details"`
	Location         string          `json:"location"`
	Photos           []Photo         `json:"photos"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
	Revision         int             `json:"revision"`
	Archived         bool            `json:"archived"`
	Intake           json.RawMessage `json:"original_intake,omitempty"`
}

func (c Asset) Label() string {
	return fmt.Sprintf("%05d", c.ID)
}
func (c Asset) Title() string {
	if c.ShortDescription == "" && c.Description == "" && c.Details == "" {
		return "Unidentified item"
	}
	short := c.ShortDescription
	if short == "" {
		short, _ = splitDescription(c.Description)
	}
	title := []rune(short)
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
	if version > 6 {
		return fmt.Errorf("database schema %d is newer than this Syscat supports (6)", version)
	}
	for next := version + 1; next <= 6; next++ {
		script, err := migrations.ReadFile(fmt.Sprintf("migrations/%03d.sql", next))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(script)); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", next)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const columns = `id, id, description, short_description, details, location, photos, created_at, updated_at, revision, archived, intake`

type scanner interface{ Scan(...any) error }

func scanAsset(row scanner) (Asset, error) {
	var c Asset
	var photos, intake string
	err := row.Scan(&c.ID, &c.CatalogNumber, &c.Description, &c.ShortDescription, &c.Details, &c.Location, &photos, &c.CreatedAt, &c.UpdatedAt, &c.Revision, &c.Archived, &intake)
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
	if c.ShortDescription == "" && c.Details == "" {
		c.setLegacyDescription(c.Description)
	} else {
		c.projectDescription()
	}
	if c.Photos == nil {
		c.Photos = []Photo{}
	}
	photos, err := json.Marshal(c.Photos)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	c.CreatedAt, c.UpdatedAt, c.Revision = now, now, 1
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var next, id int64
	if err := tx.QueryRow("SELECT next_id FROM asset_id_allocator WHERE singleton=1").Scan(&next); err != nil {
		return 0, err
	}
	// Every free run starts at next or immediately after an occupied ID. This
	// finds the first available slot without querying each occupied ID in turn.
	err = tx.QueryRow(`SELECT candidate FROM (
		SELECT ? AS candidate
		UNION SELECT id+1 FROM assets WHERE id>=? AND id<9223372036854775807
	) WHERE NOT EXISTS (SELECT 1 FROM assets WHERE id=candidate)
	ORDER BY candidate LIMIT 1`, next, next).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("asset ID sequence exhausted")
	}
	if err != nil {
		return 0, err
	}
	// Keep initial observations, including their allocated ID, when fields or
	// IDs change later. The compatibility field mirrors the sole asset ID.
	c.ID, c.CatalogNumber = id, id
	intake, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO assets(id, description, short_description, details, location, photos, intake, created_at, updated_at, submission_key) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, c.Description, c.ShortDescription, c.Details, c.Location, string(photos), string(intake), now, now, key); err != nil {
		return 0, err
	}
	next = id
	if id < 9223372036854775807 {
		next++
	}
	if _, err := tx.Exec("UPDATE asset_id_allocator SET next_id=? WHERE singleton=1", next); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) Update(c Asset) error {
	return s.update(c, nil)
}

func (s *Store) UpdateWithPhotoDeletions(c Asset, deleted map[string]bool) error {
	if len(deleted) == 0 {
		return s.Update(c)
	}
	original, err := s.Get(c.ID)
	if err != nil {
		return err
	}
	intake, err := pruneIntakePhotos(original.Intake, deleted)
	if err != nil {
		return err
	}
	return s.update(c, string(intake))
}

func (s *Store) update(c Asset, intake any) error {
	original, err := s.Get(c.ID)
	if err != nil {
		return err
	}
	if c.Description != original.Description && c.ShortDescription == original.ShortDescription && c.Details == original.Details {
		c.setLegacyDescription(c.Description)
	} else if c.ShortDescription != original.ShortDescription || c.Details != original.Details {
		c.projectDescription()
	}
	photos, err := json.Marshal(c.Photos)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE assets SET description=?, short_description=?, details=?, location=?, photos=?, intake=COALESCE(?, intake), updated_at=?, revision=revision+1 WHERE id=? AND revision=? AND archived=0`, c.Description, c.ShortDescription, c.Details, c.Location, string(photos), intake, time.Now().UTC().Format(time.RFC3339Nano), c.ID, c.Revision)
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

func assetSearchWhere(query string, archived bool, field string) (string, []any) {
	where := ` WHERE archived=?`
	args := []any{archived}
	if query != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
		pattern := "%" + escaped + "%"
		id, err := strconv.ParseInt(query, 10, 64)
		if err != nil || id < 1 {
			id = 0
		}
		switch field {
		case "title":
			where += ` AND short_description LIKE ? ESCAPE '\'`
			args = append(args, pattern)
		case "short_description", "details", "description", "location":
			// The column name comes only from these fixed choices.
			where += ` AND ` + field + ` LIKE ? ESCAPE '\'`
			args = append(args, pattern)
		case "caption":
			where += ` AND EXISTS (SELECT 1 FROM json_each(assets.photos) AS photo WHERE json_extract(photo.value, '$.caption') LIKE ? ESCAPE '\')`
			args = append(args, pattern)
		case "catalog_number", "id":
			where += ` AND id=?`
			args = append(args, id)
		default:
			where += ` AND ((description || ' ' || location) LIKE ? ESCAPE '\' OR id=? OR EXISTS (SELECT 1 FROM json_each(assets.photos) AS photo WHERE json_extract(photo.value, '$.caption') LIKE ? ESCAPE '\'))`
			args = append(args, pattern, id, pattern)
		}
	}
	return where, args
}

func (s *Store) List(query string, archived bool, limit, offset int) ([]Asset, int, error) {
	return s.listWithField(query, archived, limit, offset, "all")
}

func (s *Store) listWithField(query string, archived bool, limit, offset int, field string) ([]Asset, int, error) {
	where, args := assetSearchWhere(query, archived, field)
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

type AssetSummary struct {
	ID                   int64
	CatalogNumber        int64
	Title                string
	Revision, PhotoCount int
	Archived             bool
}

// Summary reads avoid loading full descriptions, intake snapshots, and photo
// arrays. Search still uses the exact predicates shared with the full listing.
func (s *Store) ListSummaries(query string, archived bool, limit, offset int) ([]AssetSummary, int, error) {
	return s.listSummariesWithField(query, archived, limit, offset, "all")
}

func (s *Store) listSummariesWithField(query string, archived bool, limit, offset int, field string) ([]AssetSummary, int, error) {
	where, args := assetSearchWhere(query, archived, field)
	var total int
	if err := s.db.QueryRow("SELECT count(*) FROM assets"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(`SELECT id, id, short_description, description='', revision, COALESCE(json_array_length(photos),0), archived FROM assets`+where+" ORDER BY id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	summaries := []AssetSummary{}
	for rows.Next() {
		var summary AssetSummary
		var firstLine string
		var emptyDescription bool
		if err := rows.Scan(&summary.ID, &summary.CatalogNumber, &firstLine, &emptyDescription, &summary.Revision, &summary.PhotoCount, &summary.Archived); err != nil {
			return nil, 0, err
		}
		summary.Title = strings.TrimSpace((Asset{Description: firstLine}).Title())
		if firstLine == "" && !emptyDescription {
			summary.Title = ""
		}
		summaries = append(summaries, summary)
	}
	return summaries, total, rows.Err()
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
