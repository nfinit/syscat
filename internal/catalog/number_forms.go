package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func formNumber(values map[string][]string, key string) (int64, error) {
	items := values[key]
	if len(items) != 1 {
		return 0, fmt.Errorf("Submit one positive integer for %s.", key)
	}
	number, err := strconv.ParseInt(items[0], 10, 64)
	if err != nil || number < 1 || strings.Trim(items[0], "0123456789") != "" {
		return 0, fmt.Errorf("Submit one positive integer for %s.", key)
	}
	return number, nil
}

func (a *App) numberEditor(w http.ResponseWriter, r *http.Request) {
	asset, ok := a.getAsset(w, r)
	if !ok {
		return
	}
	if asset.Archived {
		http.Redirect(w, r, fmt.Sprintf("/assets/%d", asset.ID), 303)
		return
	}
	session := a.getSession(w, r)
	a.render(w, 200, page{Page: "number", Title: "Change asset ID", Asset: asset, NumberTarget: asset.ID, CSRF: session.CSRF})
}

func (a *App) numberChange(w http.ResponseWriter, r *http.Request) {
	asset, ok := a.getAsset(w, r)
	if !ok {
		return
	}
	session := a.getSession(w, r)
	p := page{Page: "number", Title: "Change asset ID", Asset: asset, NumberTarget: asset.ID, CSRF: session.CSRF}
	fail := func(status int, message string) { p.Error = message; a.render(w, status, p) }
	if err := a.parsePost(w, r, session); err != nil {
		cleanupForm(r)
		fail(400, err.Error())
		return
	}
	defer cleanupForm(r)
	number, err := formNumber(r.PostForm, "catalog_number")
	if err != nil {
		fail(400, err.Error())
		return
	}
	p.NumberTarget = number
	revision, err := formNumber(r.PostForm, "revision")
	if err != nil {
		fail(400, err.Error())
		return
	}
	if int64(asset.Revision) != revision || asset.Archived {
		fail(409, "Entry changed. Reopen the number editor before trying again.")
		return
	}
	if number == asset.ID {
		http.Redirect(w, r, fmt.Sprintf("/assets/%d", asset.ID), 303)
		return
	}
	if r.PostForm.Get("confirm") != "1" {
		other, err := a.store.Get(number)
		if err == nil {
			p.NumberOther = &other
		} else if !errors.Is(err, sql.ErrNoRows) {
			a.fail(w, err)
			return
		}
		p.NumberPreview = true
		a.render(w, 200, p)
		return
	}
	if r.PostForm.Get("acknowledge_link_changes") != "1" {
		fail(400, "Acknowledge that changing IDs breaks existing links before confirming.")
		return
	}
	var swapID int64
	var swapRevision int
	if _, exists := r.PostForm["swap_id"]; exists {
		swapID, err = formNumber(r.PostForm, "swap_id")
		if err != nil {
			fail(400, err.Error())
			return
		}
		value, err := formNumber(r.PostForm, "swap_revision")
		if err != nil || value > int64(^uint(0)>>1) {
			fail(400, "Invalid swap revision.")
			return
		}
		swapRevision = int(value)
	} else if _, exists := r.PostForm["swap_revision"]; exists {
		fail(400, "Missing swap record.")
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	updated, _, err := a.store.renumber(asset.ID, asset.Revision, number, swapID, swapRevision, "browser")
	if err != nil {
		if errors.Is(err, ErrConflict) {
			fail(409, "Number assignment or records changed. Preview the change again.")
		} else {
			a.fail(w, err)
		}
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/assets/%d?updated=1", updated.ID), 303)
}
