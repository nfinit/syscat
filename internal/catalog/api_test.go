package catalog

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func readAPI(t *testing.T, result *httptest.ResponseRecorder, target any) {
	t.Helper()
	if result.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("non-JSON API response: %s", result.Header())
	}
	if result.Header().Get("Set-Cookie") != "" || result.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected API session or caching headers: %s", result.Header())
	}
	if err := json.Unmarshal(result.Body.Bytes(), target); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, result.Body.String())
	}
}

func TestAPIReadsSearchAndPagination(t *testing.T) {
	app, b, _ := start(t)
	photo := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Original <desktop>\nUnknown model."}}, photo, photo), 303)
	c, err := app.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	c.Description, c.Location = "Desktop café\n100% _ literal and markings.", "Shelf B"
	if err := app.store.Update(c); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.Create(Asset{Description: "Portable system"}, randomKey()); err != nil {
		t.Fatal(err)
	}
	archivedID, err := app.store.Create(Asset{Description: "Archived system"}, randomKey())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Archive(archivedID, 1, true); err != nil {
		t.Fatal(err)
	}
	b.cookie = nil
	sessions := len(app.sessions)

	result := b.get("/api/assets?page_size=1")
	expect(t, result, 200)
	var first apiAssetList
	readAPI(t, result, &first)
	if first.Total != 2 || first.Page != 1 || first.PageSize != 1 || first.Archived || len(first.Assets) != 1 || first.Assets[0].ID != 2 || first.Previous != "" || first.Next == "" {
		t.Fatalf("invalid first page: %+v", first)
	}
	if first.Assets[0].Photos == nil || first.Assets[0].Intake != nil {
		t.Fatal("list arrays or snapshot omission incorrect")
	}
	result = b.get(first.Next)
	expect(t, result, 200)
	var second apiAssetList
	readAPI(t, result, &second)
	if len(second.Assets) != 1 || second.Assets[0].ID != 1 || second.Previous == "" || second.Next != "" {
		t.Fatalf("invalid second page: %+v", second)
	}
	result = b.get(second.Previous)
	expect(t, result, 200)

	for _, query := range []string{"1", "00001", "Shelf B", "Desktop", "% _"} {
		result = b.get("/api/assets?q=" + url.QueryEscape(query))
		expect(t, result, 200)
		var found apiAssetList
		readAPI(t, result, &found)
		if found.Total != 1 || len(found.Assets) != 1 || found.Assets[0].ID != 1 || found.Query != query {
			t.Fatalf("search %q: %+v", query, found)
		}
	}
	result = b.get("/api/assets?q=missing")
	var empty apiAssetList
	readAPI(t, result, &empty)
	if empty.Total != 0 || empty.Assets == nil || len(empty.Assets) != 0 {
		t.Fatal("empty API list must use []")
	}
	for _, suffix := range []string{"", "/edit", "/archive"} {
		if suffix == "" {
			continue
		}
		result = b.get("/api/assets/1" + suffix)
		expect(t, result, 404)
	}
	for _, archiveValue := range []string{"1", "true"} {
		result = b.get("/api/assets?archived=" + archiveValue)
		expect(t, result, 200)
		var archive apiAssetList
		readAPI(t, result, &archive)
		if !archive.Archived || archive.Total != 1 || archive.Assets[0].ID != archivedID {
			t.Fatalf("archive: %+v", archive)
		}
	}

	for _, path := range []string{"/api/assets/1", "/api/assets/00001"} {
		result = b.get(path)
		expect(t, result, 200)
		var detail apiAsset
		readAPI(t, result, &detail)
		if detail.ID != 1 || detail.Label != "00001" || detail.Title != "Desktop café" || detail.Description != c.Description || detail.Location != "Shelf B" || detail.Revision != 2 || detail.Archived || detail.URL != "/assets/1" || detail.APIURL != "/api/assets/1" {
			t.Fatalf("detail mismatch: %+v", detail)
		}
		var original Asset
		if err := json.Unmarshal(detail.Intake, &original); err != nil || original.Description != "Original <desktop>\nUnknown model." {
			t.Fatal("original intake missing or changed", err)
		}
		if len(detail.Photos) != 2 || detail.Photos[0].Role != "overview" || detail.Photos[1].Role != "detail" {
			t.Fatal("photo roles or order incorrect")
		}
		for _, p := range detail.Photos {
			original := b.get(p.OriginalURL)
			expect(t, original, 200)
			if !bytes.Equal(original.Body.Bytes(), photo) {
				t.Fatal("original image URL did not preserve bytes")
			}
			thumbnail := b.get(p.ThumbnailURL)
			expect(t, thumbnail, 200)
			if thumbnail.Header().Get("Content-Type") != "image/jpeg" {
				t.Fatal("thumbnail URL did not return JPEG")
			}
		}
	}
	result = b.get("/api/assets/3")
	expect(t, result, 200)
	var archived apiAsset
	readAPI(t, result, &archived)
	if !archived.Archived || archived.Revision != 2 {
		t.Fatal("archived detail incorrect")
	}
	if len(app.sessions) != sessions {
		t.Fatal("API reads created browser sessions")
	}
}

func TestAPIRejectsInvalidRequestsAndWrites(t *testing.T) {
	app, b, _ := start(t)
	id, err := app.store.Create(Asset{Description: "Preserved system"}, randomKey())
	if err != nil || id != 1 {
		t.Fatal(err)
	}
	b.cookie = nil
	cases := []struct {
		path   string
		status int
		code   string
	}{
		{"/api/assets?page=0", 400, "invalid_request"},
		{"/api/assets?page=-1", 400, "invalid_request"},
		{"/api/assets?page=1000001", 400, "invalid_request"},
		{"/api/assets?page=bad", 400, "invalid_request"},
		{"/api/assets?page=", 400, "invalid_request"},
		{"/api/assets?page_size=0", 400, "invalid_request"},
		{"/api/assets?page_size=101", 400, "invalid_request"},
		{"/api/assets?archived=maybe", 400, "invalid_request"},
		{"/api/assets?page=1&page=2", 400, "invalid_request"},
		{"/api/assets?unknown=1", 400, "invalid_request"},
		{"/api/assets?q=%zz", 400, "invalid_request"},
		{"/api/assets/nope", 400, "invalid_id"},
		{"/api/assets/0", 400, "invalid_id"},
		{"/api/assets/-1", 400, "invalid_id"},
		{"/api/assets/+1", 400, "invalid_id"},
		{"/api/assets/9223372036854775808", 400, "invalid_id"},
		{"/api/assets/999", 404, "not_found"},
		{"/api/unknown", 404, "not_found"},
		{"/api", 404, "not_found"},
	}
	for _, tc := range cases {
		result := b.get(tc.path)
		expect(t, result, tc.status)
		var response apiErrorResponse
		readAPI(t, result, &response)
		if response.Error.Code != tc.code || response.Error.Message == "" {
			t.Fatalf("%s: %+v", tc.path, response)
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		for _, path := range []string{"/api/assets", "/api/assets/1", "/api/openapi.json"} {
			result := b.request(method, path, "application/json", strings.NewReader(`{"description":"Overwrite attempt"}`))
			expect(t, result, http.StatusMethodNotAllowed)
			var response apiErrorResponse
			readAPI(t, result, &response)
			if response.Error.Code != "method_not_allowed" || result.Header().Get("Allow") != "GET, HEAD" {
				t.Fatalf("incorrect method response: %+v", response)
			}
		}
	}
	c, err := app.store.Get(1)
	_, count, listErr := app.store.List("", false, 50, 0)
	if err != nil || listErr != nil || c.Description != "Preserved system" || c.Revision != 1 || c.Archived || count != 1 {
		t.Fatal("API writes changed inventory")
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	result := b.get("/api/assets")
	expect(t, result, 500)
	var response apiErrorResponse
	readAPI(t, result, &response)
	if response.Error.Code != "internal_error" || strings.Contains(result.Body.String(), "database") {
		t.Fatal("internal error leaked database details")
	}
}

func TestAPIPaginationLinksAndSpec(t *testing.T) {
	app, b, _ := start(t)
	for range 3 {
		id, err := app.store.Create(Asset{Description: "Archive % _"}, randomKey())
		if err != nil {
			t.Fatal(err)
		}
		if err := app.store.Archive(id, 1, true); err != nil {
			t.Fatal(err)
		}
	}
	b.cookie = nil
	result := b.get("/api/assets?q=%25%20_&archived=true&page_size=2")
	var first apiAssetList
	readAPI(t, result, &first)
	if first.Total != 3 || len(first.Assets) != 2 {
		t.Fatalf("filtered pagination: %+v", first)
	}
	next, err := url.Parse(first.Next)
	if err != nil || next.Query().Get("q") != "% _" || next.Query().Get("archived") != "1" || next.Query().Get("page_size") != "2" || next.Query().Get("page") != "2" {
		t.Fatal("pagination link lost filters", err)
	}
	result = b.get(first.Next)
	var second apiAssetList
	readAPI(t, result, &second)
	if second.Total != 3 || len(second.Assets) != 1 || !second.Archived || second.Next != "" || second.Previous == "" {
		t.Fatalf("filtered second page: %+v", second)
	}
	result = b.get("/api/openapi.json")
	expect(t, result, 200)
	var spec struct {
		OpenAPI string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	readAPI(t, result, &spec)
	if spec.OpenAPI != "3.0.3" {
		t.Fatal("incorrect OpenAPI version")
	}
	for _, path := range []string{"/api/assets", "/api/assets/{id}", "/api/openapi.json", "/photos/{name}", "/thumbnails/{name}"} {
		if _, ok := spec.Paths[path]; !ok {
			t.Fatalf("spec missing path %s", path)
		}
	}
	for _, path := range []string{"/api/assets", "/api/assets/1", "/api/openapi.json"} {
		result := b.request("HEAD", path, "", nil)
		expect(t, result, 200)
		if result.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatal("HEAD content type incorrect")
		}
	}
}
