package catalog

import (
	"net/url"
	"strings"
	"testing"
)

func TestCaptionSearchMatchesCurrentPhotosOnce(t *testing.T) {
	app, b, _ := start(t)
	image := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Desktop"}, "caption_overview": {"Unknown chip 100% _ \\ detail"}, "caption_photos": {"Unknown chip closeup"}}, image, image), 303)
	second, err := app.store.Create(Asset{Description: "Portable", Photos: []Photo{{Path: "photos/second.jpg", Caption: "UNKNOWN CHIP"}}}, randomKey())
	if err != nil {
		t.Fatal(err)
	}
	archived, err := app.store.Create(Asset{Description: "Archived", Photos: []Photo{{Caption: "Unknown chip"}}}, randomKey())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.Archive(archived, 1, true); err != nil {
		t.Fatal(err)
	}
	check := func(query string, archive bool, want ...int64) {
		t.Helper()
		assets, count, err := app.store.List(query, archive, 50, 0)
		if err != nil || count != len(want) || len(assets) != len(want) {
			t.Fatalf("search %q: count=%d assets=%v err=%v", query, count, assets, err)
		}
		for i, id := range want {
			if assets[i].ID != id {
				t.Fatalf("search %q: got ID %d want %d", query, assets[i].ID, id)
			}
		}
	}
	check("unknown CHIP", false, second, 1)
	check("unknown chip", true, archived)
	check(`% _ \`, false, 1)
	check("Desktop", false, 1)
	check("00001", false, 1)
	check("second.jpg", false) // Filenames and paths are not searchable captions.
	var first apiAssetList
	result := b.get("/api/assets?q=unknown+chip&page_size=1")
	expect(t, result, 200)
	readAPI(t, result, &first)
	if first.Total != 2 || len(first.Assets) != 1 || first.Assets[0].ID != second || first.Next == "" {
		t.Fatalf("caption API pagination: %+v", first)
	}
	var next apiAssetList
	readAPI(t, b.get(first.Next), &next)
	if next.Total != 2 || len(next.Assets) != 1 || next.Assets[0].ID != 1 || next.Next != "" {
		t.Fatalf("caption API next page: %+v", next)
	}
	html := b.get("/assets?q=unknown+chip")
	expect(t, html, 200)
	if !strings.Contains(html.Body.String(), "Desktop") || !strings.Contains(html.Body.String(), "Portable") || strings.Contains(html.Body.String(), ">Archived<") {
		t.Fatal("HTML search differs from API")
	}
	// Editing current captions immediately changes matches, without searching intake.
	c, err := app.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	c.Photos[0].Caption = "Identified controller"
	c.Photos[1].Caption = "Remove this caption"
	if err := app.store.Update(c); err != nil {
		t.Fatal(err)
	}
	check("unknown chip", false, second)
	check("identified controller", false, 1)
	check("remove this caption", false, 1)
	expect(t, b.post("/assets/1", url.Values{"revision": {"2"}, "description": {"Desktop"}, "delete_photo": {c.Photos[1].Path}}), 303)
	check("remove this caption", false)
	var deleted apiAssetList
	readAPI(t, b.get("/api/assets?q=remove+this+caption"), &deleted)
	if deleted.Total != 0 || len(deleted.Assets) != 0 {
		t.Fatal("deleted caption still matches API")
	}
	// Legacy photos missing a caption and entries with no photos remain readable.
	if _, err := app.store.Create(Asset{Description: "Legacy", Photos: []Photo{{Path: "photos/legacy.jpg"}}}, randomKey()); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.Create(Asset{Description: "No photos"}, randomKey()); err != nil {
		t.Fatal(err)
	}
	check("unknown chip", false, second)
}
