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
	values, ok := apiPatchObject(w, r, "catalog_number", "swap_id", "swap_revision")
	if !ok {
		return
	}
	revision, err := apiPatchRevision(values)
	if err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	number, err := apiPositiveNumber(values, "catalog_number")
	if err != nil {
		apiError(w, 400, "invalid_request", err.Error())
		return
	}
	var swapID int64
	var swapRevision int
	_, hasID := values["swap_id"]
	_, hasRevision := values["swap_revision"]
	if hasID != hasRevision {
		apiError(w, 400, "invalid_request", "Supply both swap_id and swap_revision for an occupied catalog number.")
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
	asset, swapped, err := a.store.Renumber(current.ID, revision, number, swapID, swapRevision)
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
	apiJSON(w, http.StatusOK, result)
}
