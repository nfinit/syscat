package catalog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func apiUploadBody(t *testing.T, values url.Values, files map[string][][]byte) (string, []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, items := range values {
		for _, value := range items {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	for key, items := range files {
		for i, data := range items {
			part, err := writer.CreateFormFile(key, fmt.Sprintf("photo-%d.jpg", i))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return writer.FormDataContentType(), body.Bytes()
}

func apiUpload(t *testing.T, b *browser, path string, values url.Values, files ...[]byte) *httptest.ResponseRecorder {
	t.Helper()
	content, body := apiUploadBody(t, values, map[string][][]byte{"photos": files})
	return b.request("POST", path, content, bytes.NewReader(body))
}

func photoFiles(t *testing.T, dir string) []string {
	t.Helper()
	var result []string
	for _, folder := range []string{"photos", "thumbnails"} {
		entries, err := os.ReadDir(filepath.Join(dir, folder))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			result = append(result, folder+"/"+entry.Name())
		}
	}
	return result
}

func TestAPIPhotoUploadLifecycleAndPrivacy(t *testing.T) {
	app, b, dir := start(t)
	imageBytes := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Initial system"}, "location": {"Workshop"}, "group_photos": {"Interior", "Exterior"}}, imageBytes, imageBytes, imageBytes), 303)
	initial, _ := app.store.Get(1)
	b.cookie = nil
	sessions := len(app.sessions)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), encoded.Bytes()[:2]...)
	raw = append(raw, jpegSegment(0xe1, gpsEXIF(binary.LittleEndian, 6))...)
	raw = append(raw, encoded.Bytes()[2:]...)
	response := apiUpload(t, b, "/api/assets/1/photos", url.Values{"revision": {"1"}, "caption_photos": {"  Identified chip  ", "Rear"}, "group_photos": {"Interior", ""}}, raw, imageBytes)
	expect(t, response, 201)
	if response.Header().Get("Location") != "/api/assets/1" || len(response.Result().Cookies()) != 0 || len(app.sessions) != sessions {
		t.Fatal("upload created session or omitted location")
	}
	var saved apiAsset
	readAPI(t, response, &saved)
	if saved.Revision != 2 || len(saved.Photos) != 5 || saved.PhotoUploadURL != "/api/assets/1/photos" {
		t.Fatal(saved)
	}
	if saved.Photos[0].ID != initial.Photos[0].ID() || saved.Photos[1].ID != initial.Photos[1].ID() || saved.Photos[2].Caption != "Identified chip" || saved.Photos[2].Group != "Interior" || saved.Photos[3].ID != initial.Photos[2].ID() {
		t.Fatal("group/overview order changed incorrectly", saved.Photos)
	}
	if saved.Description != initial.Description || saved.Location != initial.Location || saved.CreatedAt != initial.CreatedAt || string(saved.Intake) != string(initial.Intake) {
		t.Fatal("upload overwrote text/intake")
	}
	current, _ := app.store.Get(1)
	sanitized, err := stripPhotoMetadata(raw, "jpeg")
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(dir, current.Photos[2].Path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, sanitized) || bytes.Contains(original, []byte("PRIVATE GPS")) {
		t.Fatal("unsanitized API original")
	}
	thumb, err := os.Open(filepath.Join(dir, current.Photos[2].Thumbnail))
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(thumb)
	thumb.Close()
	if err != nil || config.Width != 20 || config.Height != 40 {
		t.Fatal("API thumbnail lost orientation", config, err)
	}
	for _, photo := range saved.Photos {
		expect(t, b.get(photo.OriginalURL), 200)
		expect(t, b.get(photo.ThumbnailURL), 200)
	}
	var reread apiAsset
	readAPI(t, b.get("/api/assets/1"), &reread)
	if !reflect.DeepEqual(saved, reread) {
		t.Fatal("upload not persisted")
	}
	// Uploaded photos can immediately use existing caption/group PATCH calls.
	patch := b.request("PATCH", saved.Photos[2].APIURL, "application/json", strings.NewReader(`{"revision":2,"caption":"Verified chip"}`))
	expect(t, patch, 200)
	// A plain file-only upload needs no caption/group fields.
	expect(t, apiUpload(t, b, "/api/assets/1/photos", url.Values{"revision": {"3"}}, imageBytes), 201)
	final, _ := app.store.Get(1)
	if final.Revision != 4 || len(final.Photos) != 6 || final.Photos[5].Caption != "" || final.Photos[5].Group != "" {
		t.Fatal(final)
	}
}

func TestAPIPhotoUploadFailuresAreAtomic(t *testing.T) {
	app, b, dir := start(t)
	imageBytes := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Original"}}, imageBytes), 303)
	b.cookie = nil
	before, _ := app.store.Get(1)
	filesBefore := photoFiles(t, dir)
	cases := []struct {
		path   string
		values url.Values
		files  map[string][][]byte
		status int
	}{
		{"/api/assets/1/photos", nil, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1", "1"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1.0"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"0"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}}, nil, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}, "description": {"Overwrite"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}, "photos": {"not a file"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}}, map[string][][]byte{"overview": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}}, map[string][][]byte{"photos": {imageBytes, []byte("invalid image")}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}, "caption_photos": {"one"}}, map[string][][]byte{"photos": {imageBytes, imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}, "group_photos": {"one", "two"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}, "caption_photos": {strings.Repeat("x", 1001)}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"1"}, "group_photos": {"two\nlines"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos", url.Values{"revision": {"2"}}, map[string][][]byte{"photos": {imageBytes}}, 409},
		{"/api/assets/999/photos", url.Values{"revision": {"1"}}, map[string][][]byte{"photos": {imageBytes}}, 404},
		{"/api/assets/abc/photos", url.Values{"revision": {"1"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
		{"/api/assets/1/photos?revision=1", url.Values{"revision": {"1"}}, map[string][][]byte{"photos": {imageBytes}}, 400},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			content, body := apiUploadBody(t, tc.values, tc.files)
			response := b.request("POST", tc.path, content, bytes.NewReader(body))
			expect(t, response, tc.status)
			var failure apiErrorResponse
			readAPI(t, response, &failure)
			if failure.Error.Code == "" {
				t.Fatal("not a JSON error")
			}
			after, _ := app.store.Get(1)
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(filesBefore, photoFiles(t, dir)) {
				t.Fatal("failed batch changed record/files")
			}
		})
	}
	expect(t, b.request("POST", "/api/assets/1/photos", "application/json", strings.NewReader(`{}`)), 415)
	expect(t, b.request("POST", "/api/assets/1/photos", "multipart/form-data; boundary=broken", strings.NewReader("broken")), 400)
	for _, method := range []string{"GET", "HEAD", "PATCH", "DELETE"} {
		response := b.request(method, "/api/assets/1/photos", "", nil)
		expect(t, response, 405)
		if response.Header().Get("Allow") != "POST" {
			t.Fatal("incorrect Allow")
		}
	}
	// A database failure after files have been generated must remove them too.
	if _, err := app.store.db.Exec(`CREATE TRIGGER fail_upload BEFORE UPDATE ON assets BEGIN SELECT RAISE(ABORT,'test save failure'); END`); err != nil {
		t.Fatal(err)
	}
	expect(t, apiUpload(t, b, "/api/assets/1/photos", url.Values{"revision": {"1"}}, imageBytes), 500)
	if !reflect.DeepEqual(filesBefore, photoFiles(t, dir)) {
		t.Fatal("failed DB save leaked files")
	}
	if _, err := app.store.db.Exec(`DROP TRIGGER fail_upload`); err != nil {
		t.Fatal(err)
	}
	if err := app.store.Archive(1, 1, true); err != nil {
		t.Fatal(err)
	}
	expect(t, apiUpload(t, b, "/api/assets/1/photos", url.Values{"revision": {"2"}}, imageBytes), 409)
	if !reflect.DeepEqual(filesBefore, photoFiles(t, dir)) {
		t.Fatal("archived upload saved files")
	}
}

func TestAPIPhotoUploadLimitsPolicyAndConcurrency(t *testing.T) {
	app, b, dir := startWithUploadLimit(t, 1)
	imageBytes := pngPhoto(t)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Original"}}, imageBytes), 303)
	multipartTemp := t.TempDir()
	t.Setenv("TMPDIR", multipartTemp)
	filesBefore := photoFiles(t, dir)
	b.cookie = nil
	expect(t, apiUpload(t, b, "/api/assets/1/photos", url.Values{"revision": {"1"}}, make([]byte, 2<<20)), 413)
	app.maxUploadMiB = 16
	expect(t, apiUpload(t, b, "/api/assets/1/photos", url.Values{"revision": {"1"}}, make([]byte, maxPhotoBytes+1)), 413)
	if !reflect.DeepEqual(filesBefore, photoFiles(t, dir)) {
		t.Fatal("oversized upload saved files")
	}
	temporary, err := os.ReadDir(multipartTemp)
	if err != nil || len(temporary) != 0 {
		t.Fatal("upload leaked multipart temporary files", temporary, err)
	}
	app.apiWritePolicy = func(w http.ResponseWriter, r *http.Request) bool {
		apiError(w, 403, "forbidden", "test policy")
		return false
	}
	expect(t, b.request("POST", "/api/assets/1/photos", "", strings.NewReader("unparsed")), 403)
	app.apiWritePolicy = trustedAPIWrite
	content, body := apiUploadBody(t, url.Values{"revision": {"1"}}, map[string][][]byte{"photos": {imageBytes}})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &browser{app: app}
			statuses <- client.request("POST", "/api/assets/1/photos", content, bytes.NewReader(body)).Code
		}()
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	after, _ := app.store.Get(1)
	if counts[201] != 1 || counts[409] != 1 || after.Revision != 2 || len(after.Photos) != 2 || len(photoFiles(t, dir)) != 4 {
		t.Fatal("concurrent uploads lost revision guard or leaked files", counts, after)
	}
}

func TestAPIPhotoUploadLegacyEmptyRecord(t *testing.T) {
	app, b, dir := start(t)
	if _, err := app.store.Create(Asset{Description: "Legacy record"}, randomKey()); err != nil {
		t.Fatal(err)
	}
	before := photoFiles(t, dir)
	photo := pngPhoto(t)
	expect(t, apiUpload(t, b, "/api/assets/1/photos", url.Values{"revision": {"1"}, "group_photos": {"Interior"}}, photo), 400)
	if !reflect.DeepEqual(before, photoFiles(t, dir)) {
		t.Fatal("rejected legacy overview left files")
	}
	response := apiUpload(t, b, "/api/assets/1/photos", url.Values{"revision": {"1"}}, photo)
	expect(t, response, 201)
	var saved apiAsset
	readAPI(t, response, &saved)
	if len(saved.Photos) != 1 || saved.Photos[0].Role != "overview" || saved.Photos[0].ID == "" || saved.Revision != 2 {
		t.Fatal(saved)
	}
}
