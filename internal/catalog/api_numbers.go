package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type apiNumberResult struct {
	Asset        apiAsset  `json:"asset"`
	SwappedAsset *apiAsset `json:"swapped_asset,omitempty"`
}

func apiPositiveNumber(values map[string]json.RawMessage, key string) (int64, error) {
	var number int64
	raw, ok := values[key]
	if !ok || json.Unmarshal(raw, &number) != nil || number < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return number, nil
}

func (a *App) apiNumberChange(w http.ResponseWriter, r *http.Request) {
	values, ok := apiPatchObject(w, r, "id", "catalog_number", "swap_id", "swap_revision", "acknowledge_link_changes")
	if !ok {
		return
	}
	revision, err := apiPatchRevision(values)
	if err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	var acknowledged bool
	if json.Unmarshal(values["acknowledge_link_changes"], &acknowledged) != nil || !acknowledged {
		apiError(w, 400, "invalid_request", "Set acknowledge_link_changes to true: renumbering changes IDs and breaks existing links.")
		return
	}
	key := "id"
	if _, legacy := values["catalog_number"]; legacy {
		if _, mixed := values["id"]; mixed {
			apiError(w, 400, "invalid_request", "Supply id or its legacy catalog_number alias, not both.")
			return
		}
		key = "catalog_number"
	}
	number, err := apiPositiveNumber(values, key)
	if err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	var swapID int64
	var swapRevision int
	_, hasID := values["swap_id"]
	_, hasRevision := values["swap_revision"]
	if hasID != hasRevision {
		apiError(w, 400, "invalid_request", "Supply both swap_id and swap_revision for an occupied asset ID.")
		return
	}
	if hasID {
		swapID, err = apiPositiveNumber(values, "swap_id")
		if err == nil {
			swapRevision, err = apiPatchRevision(map[string]json.RawMessage{"revision": values["swap_revision"]})
		}
		if err != nil {
			apiError(w, 400, "invalid_request", err.Error())
			return
		}
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	current, ok := a.apiWriteAsset(w, r, revision)
	if !ok {
		return
	}
	asset, swapped, err := a.store.renumber(current.ID, revision, number, swapID, swapRevision, "api")
	if err != nil {
		if errors.Is(err, ErrConflict) {
			apiError(w, 409, "conflict", "Number assignment or records changed. Read both records and supply the current occupant's swap_id and swap_revision for a swap.")
		} else {
			apiFailure(w, err)
		}
		return
	}
	result := apiNumberResult{Asset: asAPIAsset(asset)}
	if swapped != nil {
		other := asAPIAsset(*swapped)
		result.SwappedAsset = &other
	}
	w.Header().Set("Location", result.Asset.APIURL)
	apiJSON(w, http.StatusOK, result)
}
