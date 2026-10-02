package catalog

import (
	"database/sql"
	"errors"
	"time"
)

// Renumber moves or swaps primary keys. Negative IDs are temporary slots inside
// this transaction; original observations and immutable image paths stay intact.
func (s *Store) Renumber(id int64, revision int, number, swapID int64, swapRevision int) (Asset, *Asset, error) {
	return s.renumber(id, revision, number, swapID, swapRevision, "internal")
}

func (s *Store) renumber(id int64, revision int, number, swapID int64, swapRevision int, origin string) (Asset, *Asset, error) {
	if number < 1 {
		return Asset{}, nil, errInvalidEdit
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Asset{}, nil, err
	}
	defer tx.Rollback()
	asset, err := scanAsset(tx.QueryRow("SELECT "+columns+" FROM assets WHERE id=?", id))
	if err != nil {
		return Asset{}, nil, err
	}
	if asset.Revision != revision || asset.Archived {
		return Asset{}, nil, ErrConflict
	}
	if asset.ID == number {
		if swapID != 0 || swapRevision != 0 {
			return Asset{}, nil, ErrConflict
		}
		return asset, nil, tx.Commit()
	}
	other, err := scanAsset(tx.QueryRow("SELECT "+columns+" FROM assets WHERE id=?", number))
	occupied := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Asset{}, nil, err
	}
	if occupied {
		if other.ID != swapID || other.Revision != swapRevision {
			return Asset{}, nil, ErrConflict
		}
	} else if swapID != 0 || swapRevision != 0 {
		return Asset{}, nil, ErrConflict
	}
	var sourceKey string
	if err := tx.QueryRow("SELECT submission_key FROM assets WHERE id=?", asset.ID).Scan(&sourceKey); err != nil {
		return Asset{}, nil, err
	}
	var swappedKey, swappedTitle, swappedRevision, swappedNewRevision any
	if occupied {
		var key string
		if err := tx.QueryRow("SELECT submission_key FROM assets WHERE id=?", other.ID).Scan(&key); err != nil {
			return Asset{}, nil, err
		}
		swappedKey, swappedTitle = key, other.ShortDescription
		swappedRevision, swappedNewRevision = other.Revision, other.Revision+1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec("UPDATE assets SET id=? WHERE id=?", -asset.ID, asset.ID); err != nil {
		return Asset{}, nil, err
	}
	var swapped *Asset
	if occupied {
		if _, err := tx.Exec("UPDATE assets SET id=?,revision=revision+1,updated_at=? WHERE id=?", asset.ID, now, other.ID); err != nil {
			return Asset{}, nil, err
		}
		other.ID = asset.ID
		other.CatalogNumber = other.ID
		other.Revision++
		other.UpdatedAt = now
		swapped = &other
	}
	if _, err := tx.Exec("UPDATE assets SET id=?,revision=revision+1,updated_at=? WHERE id=?", number, now, -asset.ID); err != nil {
		return Asset{}, nil, err
	}
	asset.ID = number
	asset.CatalogNumber = number
	asset.Revision++
	asset.UpdatedAt = now
	if _, err := tx.Exec(`INSERT INTO asset_id_changes(occurred_at,origin,old_id,new_id,source_key,source_title,source_revision,source_new_revision,swapped_key,swapped_title,swapped_revision,swapped_new_revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, now, origin, id, number, sourceKey, asset.ShortDescription, revision, revision+1, swappedKey, swappedTitle, swappedRevision, swappedNewRevision); err != nil {
		return Asset{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Asset{}, nil, err
	}
	return asset, swapped, nil
}
