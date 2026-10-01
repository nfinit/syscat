package catalog

import (
	"errors"
	"fmt"
)

var errInvalidEdit = errors.New("invalid entry")

// Both form and API edits pass through this boundary. Callers hold writeMu for
// their complete read/modify/save operation; the store also checks the revision.
func (a *App) saveAssetEdits(c Asset, deleted map[string]bool) error {
	if err := validate(c); err != nil {
		return fmt.Errorf("%w: %s", errInvalidEdit, err)
	}
	if len(c.Photos) == 0 {
		return fmt.Errorf("%w: keep at least one photo or upload a replacement", errInvalidEdit)
	}
	for i, photo := range c.Photos {
		if err := validateCaption(photo.Caption); err != nil {
			return fmt.Errorf("%w: %s", errInvalidEdit, err)
		}
		if err := validateGroup(photo.Group); err != nil {
			return fmt.Errorf("%w: %s", errInvalidEdit, err)
		}
		if i == 0 && photo.Group != "" {
			return fmt.Errorf("%w: the overview photo cannot belong to a group", errInvalidEdit)
		}
	}
	return a.store.UpdateWithPhotoDeletions(c, deleted)
}
