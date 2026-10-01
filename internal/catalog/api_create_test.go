package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func apiCreate(t *testing.T, b *browser, key string, values url.Values, files map[string][][]byte) *httptest.ResponseRecorder {
	t.Helper()
	content, body := apiUploadBody(t, values, files)
	request := httptest.NewRequest("POST", "/api/assets", bytes.NewReader(body))
	request.Header.Set("Content-Type", content)
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	result := httptest.NewRecorder()
	b.app.ServeHTTP(result, request)
	return result
}

func TestAPICreationAndOriginalIntake(t *testing.T) {
	app, b, dir := start(t)
	b.cookie = nil
	sessions := len(app.sessions)
	photo := pngPhoto(t)
	response := apiCreate(t, b, "first-agent-system", url.Values{"description": {"  New system\nOwner observations  "}, "location": {" Shelf A "}, "caption_overview": {" Front overview "}, "caption_photos": {"Rear", "Board", "Detail"}, "group_photos": {"Exterior", "Interior", "Exterior"}}, map[string][][]byte{"overview": {photo}, "photos": {photo, photo, photo}})
	expect(t, response, 201)
	var created apiAsset
	readAPI(t, response, &created)
	if created.ID != 1 || created.Revision != 1 || created.Description != "New system\nOwner observations" || created.Location != "Shelf A" || len(created.Photos) != 4 || created.Photos[0].Role != "overview" || created.Photos[0].Group != "" || created.Photos[0].Caption != "Front overview" {
		t.Fatal(created)
	}
	if created.Photos[1].Caption != "Rear" || created.Photos[2].Caption != "Detail" || created.Photos[3].Caption != "Board" {
		t.Fatal("intake grouping incorrect", created.Photos)
	}
	if response.Header().Get("Location") != created.APIURL || len(response.Result().Cookies()) != 0 || len(app.sessions) != sessions || len(photoFiles(t, dir)) != 8 {
		t.Fatal("creation response/session/files incorrect")
	}
	var intake Asset
	if err := json.Unmarshal(created.Intake, &intake); err != nil {
		t.Fatal(err)
	}
	current, err := app.store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if intake.Description != created.Description || intake.Location != created.Location || intake.Revision != 1 || !reflect.DeepEqual(intake.Photos, current.Photos) {
		t.Fatal("initial observations not captured")
	}
	for _, p := range created.Photos {
		if p.ID == "" {
			t.Fatal("missing stable ID")
		}
		expect(t, b.get(p.OriginalURL), 200)
		expect(t, b.get(p.ThumbnailURL), 200)
	}
	// The newly created record immediately supports attachment and text edits.
	expect(t, apiUpload(t, b, created.PhotoUploadURL, url.Values{"revision": {"1"}}, photo), 201)
	expect(t, b.request("PATCH", created.APIURL, "application/json", strings.NewReader(`{"revision":2,"description":"Researched identification"}`)), 200)
	beforeFiles := photoFiles(t, dir)
	// A key identifies one logical creation. A replay does not reinterpret new
	// observations or overwrite the original request, even after later edits.
	replay := apiCreate(t, b, "first-agent-system", url.Values{"description": {"Different replay text"}}, map[string][][]byte{"overview": {photo}})
	expect(t, replay, 200)
	var repeated apiAsset
	readAPI(t, replay, &repeated)
	if repeated.ID != 1 || repeated.Revision != 3 || repeated.Description != "Researched identification" || string(repeated.Intake) != string(created.Intake) || !reflect.DeepEqual(beforeFiles, photoFiles(t, dir)) {
		t.Fatal("replay changed record/intake/files", repeated)
	}
	app.Close()
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	b.app = reopened
	replay = apiCreate(t, b, "first-agent-system", url.Values{"description": {"Replay after restart"}}, map[string][][]byte{"overview": {photo}})
	expect(t, replay, 200)
	if err := reopened.store.Archive(1, 3, true); err != nil {
		t.Fatal(err)
	}
	replay = apiCreate(t, b, "first-agent-system", url.Values{"description": {"Archived replay"}}, map[string][][]byte{"overview": {photo}})
	expect(t, replay, 200)
	readAPI(t, replay, &repeated)
	if !repeated.Archived || repeated.ID != 1 {
		t.Fatal("archived replay created replacement")
	}
	// No key means a new creation every time; minimal required fields suffice.
	for i := 0; i < 2; i++ {
		response = apiCreate(t, b, "", url.Values{"description": {"Another system"}}, map[string][][]byte{"overview": {photo}})
		expect(t, response, 201)
		readAPI(t, response, &repeated)
		if repeated.ID != int64(i+2) || repeated.Location != "" || len(repeated.Photos) != 1 || repeated.Revision != 1 {
			t.Fatal(repeated)
		}
	}
}

func TestAPICreationRejectsInvalidIntakeAndCleansFiles(t *testing.T) {
	app, b, dir := start(t)
	photo := pngPhoto(t)
	cases := []struct {
		values url.Values
		files  map[string][][]byte
	}{
		{nil, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"  "}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"System", "Duplicate"}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"System"}, "location": {"One", "Two"}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {strings.Repeat("x", 20001)}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"System"}, "location": {strings.Repeat("x", 501)}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"System"}}, nil},
		{url.Values{"description": {"System"}}, map[string][][]byte{"overview": {photo, photo}}},
		{url.Values{"description": {"System"}}, map[string][][]byte{"photos": {photo}}},
		{url.Values{"description": {"System"}, "revision": {"1"}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"System"}, "group_overview": {"Interior"}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"System"}}, map[string][][]byte{"overview": {photo}, "unexpected": {photo}}},
		{url.Values{"description": {"System"}, "caption_overview": {"One", "Two"}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"System"}, "caption_photos": {"Orphan caption"}}, map[string][][]byte{"overview": {photo}}},
		{url.Values{"description": {"System"}, "group_photos": {"multiline\ngroup"}}, map[string][][]byte{"overview": {photo}, "photos": {photo}}},
		{url.Values{"description": {"System"}}, map[string][][]byte{"overview": {photo}, "photos": {photo, []byte("bad image")}}},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			response := apiCreate(t, b, "failed-key", tc.values, tc.files)
			expect(t, response, 400)
			var failure apiErrorResponse
			readAPI(t, response, &failure)
			if failure.Error.Code != "invalid_request" {
				t.Fatal(failure)
			}
			_, count, err := app.store.List("", false, 100, 0)
			if err != nil || count != 0 || len(photoFiles(t, dir)) != 0 {
				t.Fatal("failed creation left records/files", count, err)
			}
		})
	}
	content, body := apiUploadBody(t, url.Values{"description": {"System"}}, map[string][][]byte{"overview": {photo}})
	for _, keys := range [][]string{{""}, {"has space"}, {strings.Repeat("a", 129)}, {"one", "two"}, {"unicode-é"}} {
		req := httptest.NewRequest("POST", "/api/assets", bytes.NewReader(body))
		req.Header.Set("Content-Type", content)
		for _, key := range keys {
			req.Header.Add("Idempotency-Key", key)
		}
		result := httptest.NewRecorder()
		app.ServeHTTP(result, req)
		expect(t, result, 400)
	}
	expect(t, b.request("POST", "/api/assets", "application/json", strings.NewReader(`{}`)), 415)
	expect(t, b.request("POST", "/api/assets?q=x", content, bytes.NewReader(body)), 400)
	// Failed persistence must remove generated images and leave the key reusable.
	if _, err := app.store.db.Exec(`CREATE TRIGGER fail_create BEFORE INSERT ON assets BEGIN SELECT RAISE(ABORT,'test create failure'); END`); err != nil {
		t.Fatal(err)
	}
	expect(t, apiCreate(t, b, "failed-key", url.Values{"description": {"System"}}, map[string][][]byte{"overview": {photo}}), 500)
	if len(photoFiles(t, dir)) != 0 {
		t.Fatal("database failure leaked files")
	}
	if _, err := app.store.db.Exec(`DROP TRIGGER fail_create`); err != nil {
		t.Fatal(err)
	}
	expect(t, apiCreate(t, b, "failed-key", url.Values{"description": {"System"}}, map[string][][]byte{"overview": {photo}}), 201)
}

func TestAPICreationPolicyLimitsAndConcurrentRetries(t *testing.T) {
	app, b, dir := startWithUploadLimit(t, 1)
	photo := pngPhoto(t)
	expect(t, apiCreate(t, b, "oversized", url.Values{"description": {"System"}}, map[string][][]byte{"overview": {make([]byte, 2<<20)}}), 413)
	app.maxUploadMiB = 16
	expect(t, apiCreate(t, b, "oversized", url.Values{"description": {"System"}}, map[string][][]byte{"overview": {make([]byte, maxPhotoBytes+1)}}), 413)
	if len(photoFiles(t, dir)) != 0 {
		t.Fatal("oversized creation left files")
	}
	app.apiWritePolicy = func(w http.ResponseWriter, r *http.Request) bool {
		apiError(w, 401, "unauthorized", "test policy")
		return false
	}
	expect(t, b.request("POST", "/api/assets", "", strings.NewReader("unparsed")), 401)
	app.apiWritePolicy = trustedAPIWrite
	content, body := apiUploadBody(t, url.Values{"description": {"Concurrent creation"}}, map[string][][]byte{"overview": {photo}, "photos": {photo}})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := httptest.NewRequest("POST", "/api/assets", bytes.NewReader(body))
			request.Header.Set("Content-Type", content)
			request.Header.Set("Idempotency-Key", "concurrent-key")
			response := httptest.NewRecorder()
			app.ServeHTTP(response, request)
			statuses <- response.Code
		}()
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	assets, total, err := app.store.List("", false, 100, 0)
	if err != nil || counts[201] != 1 || counts[200] != 1 || total != 1 || assets[0].Revision != 1 || len(assets[0].Photos) != 2 || len(photoFiles(t, dir)) != 4 {
		t.Fatal("retry race created duplicates or leaked files", counts, total, err)
	}
}
