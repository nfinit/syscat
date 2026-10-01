package catalog

import (
	"database/sql"
	"errors"
	"time"
)

func (s *Store) ByCatalogNumber(number int64) (Asset, error) {
	return scanAsset(s.db.QueryRow("SELECT "+columns+" FROM assets WHERE catalog_number=?", number))
}

// Permanent IDs and intake never change. NULL is a temporary slot inside this
// transaction, avoiding collisions with every positive user catalog number.
func (s *Store) Renumber(id int64, revision int, number, swapID int64, swapRevision int) (Asset, *Asset, error) {
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
	if asset.CatalogNumber == number {
		if swapID != 0 || swapRevision != 0 {
			return Asset{}, nil, ErrConflict
		}
		return asset, nil, tx.Commit()
	}
	other, err := scanAsset(tx.QueryRow("SELECT "+columns+" FROM assets WHERE catalog_number=?", number))
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec("UPDATE assets SET catalog_number=NULL WHERE id=?", asset.ID); err != nil {
		return Asset{}, nil, err
	}
	var swapped *Asset
	if occupied {
		if _, err := tx.Exec("UPDATE assets SET catalog_number=?,revision=revision+1,updated_at=? WHERE id=?", asset.CatalogNumber, now, other.ID); err != nil {
			return Asset{}, nil, err
		}
		other.CatalogNumber = asset.CatalogNumber
		other.Revision++
		other.UpdatedAt = now
		swapped = &other
	}
	if _, err := tx.Exec("UPDATE assets SET catalog_number=?,revision=revision+1,updated_at=? WHERE id=?", number, now, asset.ID); err != nil {
		return Asset{}, nil, err
	}
	asset.CatalogNumber = number
	asset.Revision++
	asset.UpdatedAt = now
	if err := tx.Commit(); err != nil {
		return Asset{}, nil, err
	}
	return asset, swapped, nil
}
