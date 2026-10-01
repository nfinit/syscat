package catalog

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestAPISummaryListingsAndPagination(t *testing.T) {
	app, b, _ := start(t)
	for _, asset := range []Asset{
		{Description: "First model\r\nDetailed SECRET context", Location: "Shelf A", Photos: []Photo{{Caption: "Needle chip"}, {Caption: "Needle chip detail"}}},
		{Description: "Second model\nSECRET research notes", Photos: []Photo{{Caption: "Needle chip"}}},
		{Description: "Third model", Photos: []Photo{{Caption: "Needle chip"}}},
	} {
		if _, err := app.store.Create(asset, randomKey()); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.store.Archive(3, 1, true); err != nil {
		t.Fatal(err)
	}
	b.cookie = nil
	sessions := len(app.sessions)
	result := b.get("/api/assets?view=summary&q=Needle+chip&page_size=1")
	expect(t, result, 200)
	var first apiAssetSummaryList
	readAPI(t, result, &first)
	if first.View != "summary" || first.Total != 2 || len(first.Assets) != 1 || first.Assets[0].ID != 2 || first.Assets[0].PhotoCount != 1 || first.Next == "" || first.Previous != "" {
		t.Fatalf("summary first page: %+v", first)
	}
	nextURL, err := url.Parse(first.Next)
	if err != nil {
		t.Fatal(err)
	}
	if nextURL.Query().Get("view") != "summary" || nextURL.Query().Get("q") != "Needle chip" || nextURL.Query().Get("page_size") != "1" {
		t.Fatal("pagination lost summary parameters")
	}
	var second apiAssetSummaryList
	readAPI(t, b.get(first.Next), &second)
	if second.Total != 2 || len(second.Assets) != 1 || second.Assets[0].ID != 1 || second.Assets[0].PhotoCount != 2 || second.Assets[0].Title != "First model" || second.Previous == "" || second.Next != "" {
		t.Fatalf("summary second page: %+v", second)
	}
	if strings.Contains(result.Body.String(), "SECRET") || strings.Contains(result.Body.String(), "Needle chip detail") {
		t.Fatal("summary contains detailed context")
	}
	var raw struct{ Assets []map[string]json.RawMessage }
	if err := json.Unmarshal(result.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{"id": true, "label": true, "title": true, "revision": true, "photo_count": true, "archived": true, "url": true, "api_url": true}
	if len(raw.Assets[0]) != len(expected) {
		t.Fatal("unexpected summary fields", raw.Assets[0])
	}
	for key := range raw.Assets[0] {
		if !expected[key] {
			t.Fatal("summary exposes non-summary field", key)
		}
	}
	// Follow a summary link to get the complete context before editing.
	var full apiAsset
	readAPI(t, b.get(second.Assets[0].APIURL), &full)
	if full.Revision != second.Assets[0].Revision || full.Title != second.Assets[0].Title || len(full.Photos) != second.Assets[0].PhotoCount || !strings.Contains(full.Description, "SECRET") || full.Intake == nil {
		t.Fatal("summary cannot resolve full record")
	}
	var archived apiAssetSummaryList
	readAPI(t, b.get("/api/assets?view=summary&q=Needle+chip&archived=true&page_size=1"), &archived)
	if archived.Total != 1 || len(archived.Assets) != 1 || !archived.Archived || !archived.Assets[0].Archived || archived.Assets[0].ID != 3 {
		t.Fatal("summary archive filter differs")
	}
	var empty apiAssetSummaryList
	readAPI(t, b.get("/api/assets?view=summary&q=absent"), &empty)
	if empty.Total != 0 || empty.Assets == nil || len(empty.Assets) != 0 || empty.View != "summary" {
		t.Fatal("empty summary must use []")
	}
	// An explicit full view has the same wire format as the original default.
	original := b.get("/api/assets")
	explicit := b.get("/api/assets?view=full")
	if original.Body.String() != explicit.Body.String() {
		t.Fatal("default full response changed")
	}
	if len(app.sessions) != sessions {
		t.Fatal("summary request created session")
	}
}

func TestSummaryMatchesFullSearchAndTitles(t *testing.T) {
	app, b, _ := start(t)
	for _, description := range []string{"", "\nSecond line", "Model\r\nNotes", strings.Repeat("é", 125) + "\nDetails"} {
		if _, err := app.store.Create(Asset{Description: description, Location: "100% _ literal", Photos: []Photo{{Caption: "Unique caption"}}}, randomKey()); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"", "Unique caption", "Second line", "% _", "00002"} {
		full, total, err := app.store.List(query, false, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		summary, summaryTotal, err := app.store.ListSummaries(query, false, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		if total != summaryTotal || len(full) != len(summary) {
			t.Fatal("summary count/search differs", query)
		}
		for i, asset := range full {
			if summary[i].ID != asset.ID || summary[i].Title != strings.TrimSpace(asset.Title()) || summary[i].PhotoCount != len(asset.Photos) || summary[i].Revision != asset.Revision {
				t.Fatal("summary differs from full", query, summary[i])
			}
		}
	}
	for _, suffix := range []string{"view=", "view=unknown", "view=summary&view=full"} {
		response := b.get("/api/assets?" + suffix)
		expect(t, response, 400)
		var failure apiErrorResponse
		readAPI(t, response, &failure)
		if failure.Error.Code != "invalid_request" {
			t.Fatal("invalid summary query accepted")
		}
	}
	var full apiAssetList
	var summary apiAssetSummaryList
	readAPI(t, b.get("/api/assets"), &full)
	readAPI(t, b.get("/api/assets?view=summary"), &summary)
	if !reflect.DeepEqual(full.apiListPage, summary.apiListPage) {
		t.Fatal("summary changed pagination metadata")
	}
}
