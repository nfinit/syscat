package catalog

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestAPIFieldSearch(t *testing.T) {
	app, b, _ := start(t)
	fixtures := []Asset{
		{Description: "Needle model\r\nordinary notes"},
		{Description: "Other model\nNeedle notes"},
		{Description: "Other model", Location: "Needle shelf"},
		{Description: "Other model", Photos: []Photo{{Caption: "Needle chip"}, {Caption: "Needle board"}}},
		{Description: strings.Repeat("x", 130) + "Needle\nnotes"},
		{Description: "Literal 100% _ \\ model"},
		{Description: "Archived Needle"},
	}
	for _, asset := range fixtures {
		if _, err := app.store.Create(asset, randomKey()); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.store.Archive(7, 1, true); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		field, q string
		ids      []int64
	}{
		{"all", "needle", []int64{5, 4, 3, 2, 1}},
		{"title", "needle", []int64{5, 1}},
		{"description", "needle", []int64{5, 2, 1}},
		{"location", "needle", []int64{3}},
		{"caption", "needle", []int64{4}},
		{"id", "00002", []int64{2}},
		{"id", "needle", []int64{}},
		{"id", "0", []int64{}},
		{"id", "9223372036854775808", []int64{}},
		{"title", `% _ \`, []int64{6}},
		{"title", "", []int64{6, 5, 4, 3, 2, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.field+"/"+tc.q, func(t *testing.T) {
			for _, view := range []string{"full", "summary"} {
				var result struct {
					Assets []struct{ ID int64 }
					Total  int
					Field  string
				}
				path := "/api/assets?" + url.Values{"q": {tc.q}, "field": {tc.field}, "view": {view}}.Encode()
				response := b.get(path)
				expect(t, response, 200)
				readAPI(t, response, &result)
				ids := make([]int64, 0, len(result.Assets))
				for _, a := range result.Assets {
					ids = append(ids, a.ID)
				}
				if !reflect.DeepEqual(ids, tc.ids) || result.Total != len(tc.ids) || result.Field != tc.field {
					t.Fatalf("%s: %+v, want %v", view, result, tc.ids)
				}
			}
		})
	}
	for _, suffix := range []string{"field=", "field=unknown", "field=TITLE", "field=title&field=caption", "field=description%27"} {
		response := b.get("/api/assets?" + suffix)
		expect(t, response, 400)
		var failure apiErrorResponse
		readAPI(t, response, &failure)
		if failure.Error.Code != "invalid_request" {
			t.Fatal(failure)
		}
	}
	var page apiAssetSummaryList
	readAPI(t, b.get("/api/assets?view=summary&field=title&q=needle&page_size=1"), &page)
	if page.Total != 2 || len(page.Assets) != 1 || page.Assets[0].ID != 5 {
		t.Fatal(page)
	}
	next, err := url.Parse(page.Next)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"field": "title", "view": "summary", "q": "needle", "page_size": "1"} {
		if next.Query().Get(key) != value {
			t.Fatalf("lost %s: %s", key, page.Next)
		}
	}
	readAPI(t, b.get(page.Next), &page)
	if page.Assets[0].ID != 1 || page.Previous == "" {
		t.Fatal(page)
	}
	var archived apiAssetList
	readAPI(t, b.get("/api/assets?field=title&q=needle&archived=1"), &archived)
	if archived.Total != 1 || archived.Assets[0].ID != 7 {
		t.Fatal(archived)
	}
	// Original intake is excluded even when searching a specific field.
	asset, err := app.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	asset.Description = "Renamed model"
	if err := app.store.Update(asset); err != nil {
		t.Fatal(err)
	}
	var current apiAssetList
	readAPI(t, b.get("/api/assets?field=title&q=needle"), &current)
	if current.Total != 1 || current.Assets[0].ID != 5 {
		t.Fatal(current)
	}
	// An omitted selector retains the existing broad response and wire shape.
	original := b.get("/api/assets?q=needle")
	if strings.Contains(original.Body.String(), `"field"`) {
		t.Fatal("default wire shape changed")
	}
	var broad apiAssetList
	readAPI(t, original, &broad)
	if broad.Total != 4 {
		t.Fatalf("broad total: %d", broad.Total)
	}
}
