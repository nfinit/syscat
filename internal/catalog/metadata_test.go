package catalog

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/disintegration/imaging"
)

func jpegSegment(marker byte, payload []byte) []byte {
	segment := []byte{0xff, marker, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	return append(segment, payload...)
}

func gpsEXIF(order binary.ByteOrder, orientation uint16) []byte {
	tiff := make([]byte, 56)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 2)
	order.PutUint16(tiff[10:], 0x112)
	order.PutUint16(tiff[12:], 3)
	order.PutUint32(tiff[14:], 1)
	order.PutUint16(tiff[18:], orientation)
	order.PutUint16(tiff[22:], 0x8825)
	order.PutUint16(tiff[24:], 4)
	order.PutUint32(tiff[26:], 1)
	order.PutUint32(tiff[30:], 38)
	// GPS IFD containing latitude reference N; opaque location text follows it.
	order.PutUint16(tiff[38:], 1)
	order.PutUint16(tiff[40:], 1)
	order.PutUint16(tiff[42:], 2)
	order.PutUint32(tiff[44:], 2)
	tiff[48] = 'N'
	return append(append([]byte("Exif\x00\x00"), tiff...), []byte("PRIVATE GPS COORDINATES")...)
}

func TestStripJPEGMetadataPreservesPixelsAndOrientation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 5), uint8(y * 10), 80, 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, nil); err != nil {
		t.Fatal(err)
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for orientation := uint16(1); orientation <= 8; orientation++ {
			raw := append([]byte(nil), encoded.Bytes()[:2]...)
			raw = append(raw, jpegSegment(0xe1, gpsEXIF(order, orientation))...)
			raw = append(raw, jpegSegment(0xe1, []byte("http://ns.adobe.com/xap/1.0/\x00PRIVATE GPS COORDINATES"))...)
			raw = append(raw, jpegSegment(0xed, []byte("Photoshop 3.0\x00PRIVATE GPS COORDINATES"))...)
			raw = append(raw, jpegSegment(0xfe, []byte("PRIVATE GPS COORDINATES"))...)
			raw = append(raw, encoded.Bytes()[2:len(encoded.Bytes())-2]...)
			// Metadata may occur after a scan as well as before it.
			raw = append(raw, jpegSegment(0xe1, []byte("PRIVATE GPS COORDINATES"))...)
			raw = append(raw, 0xff, 0xd9)
			raw = append(raw, []byte("PRIVATE GPS COORDINATES")...)
			cleaned, err := stripPhotoMetadata(raw, "jpeg")
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(cleaned, []byte("PRIVATE GPS")) || bytes.Contains(cleaned, []byte("Photoshop")) {
				t.Fatal("location metadata retained")
			}
			want := append([]byte(nil), encoded.Bytes()[:2]...)
			want = append(want, orientationEXIF(orientation)...)
			want = append(want, encoded.Bytes()[2:]...)
			if !bytes.Equal(cleaned, want) {
				t.Fatal("JPEG image stream changed")
			}
			before, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
			if err != nil {
				t.Fatal(err)
			}
			after, err := imaging.Decode(bytes.NewReader(cleaned), imaging.AutoOrientation(true))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(imaging.Clone(before), imaging.Clone(after)) {
				t.Fatal("oriented pixels changed")
			}
		}
	}
}

func pngChunk(kind string, payload []byte) []byte {
	chunk := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(chunk, uint32(len(payload)))
	copy(chunk[4:], kind)
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	return chunk
}

func TestStripPNGMetadataPreservesImageData(t *testing.T) {
	original := pngPhoto(t)
	raw := append([]byte(nil), original[:33]...)
	for _, kind := range []string{"eXIf", "tEXt", "iTXt", "zTXt", "vpAg"} {
		raw = append(raw, pngChunk(kind, []byte("PRIVATE GPS COORDINATES"))...)
	}
	raw = append(raw, original[33:]...)
	raw = append(raw, []byte("PRIVATE GPS COORDINATES")...)
	cleaned, err := stripPhotoMetadata(raw, "png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cleaned, original) {
		t.Fatal("PNG compressed pixels changed or metadata retained")
	}
}

func TestStripGIFMetadataPreservesAnimation(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	second := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	second.SetColorIndex(2, 2, 1)
	animation := &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{7, 12}, Disposal: []byte{1, 2}, LoopCount: 3}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatal(err)
	}
	original := encoded.Bytes()
	pos := 13
	if original[10]&0x80 != 0 {
		pos += 3 * (1 << ((original[10] & 7) + 1))
	}
	raw := append([]byte(nil), original[:pos]...)
	private := []byte("PRIVATE GPS COORDINATES")
	raw = append(raw, 0x21, 0xfe, byte(len(private)))
	raw = append(raw, private...)
	raw = append(raw, 0)
	app := append([]byte("XMP DataXMP"), byte(len(private)))
	app = append(app, private...)
	app = append(app, 0)
	raw = append(raw, 0x21, 0xff, 11)
	raw = append(raw, app...)
	raw = append(raw, original[pos:]...)
	raw = append(raw, private...)
	cleaned, err := stripPhotoMetadata(raw, "gif")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cleaned, original) {
		t.Fatal("GIF animation stream changed or metadata retained")
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(cleaned))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Image) != 2 || decoded.LoopCount != 3 || !reflect.DeepEqual(decoded.Delay, animation.Delay) || !reflect.DeepEqual(decoded.Disposal, animation.Disposal) {
		t.Fatal("animation lost")
	}
}

func TestUploadStripsLocationMetadataFromOriginalAndThumbnail(t *testing.T) {
	app, b, dir := start(t)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), encoded.Bytes()[:2]...)
	raw = append(raw, jpegSegment(0xe1, gpsEXIF(binary.LittleEndian, 6))...)
	raw = append(raw, encoded.Bytes()[2:]...)
	expect(t, b.upload("/assets", url.Values{"submission": {randomKey()}, "description": {"Privacy test"}}, raw), 303)
	asset, _ := app.store.Get(1)
	cleaned, err := stripPhotoMetadata(raw, "jpeg")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{asset.Photos[0].Path, asset.Photos[0].Thumbnail} {
		data, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("PRIVATE GPS")) {
			t.Fatal("saved image retains GPS")
		}
		decoded, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Bounds().Dx() != 20 || decoded.Bounds().Dy() != 40 {
			t.Fatal("saved image lost orientation")
		}
		if path == asset.Photos[0].Path && !bytes.Equal(data, cleaned) {
			t.Fatal("unsanitized original saved")
		}
	}
	// Malformed metadata in an otherwise decodable PNG is rejected before files
	// or record revisions are changed, rather than bypassing privacy filtering.
	png := pngPhoto(t)
	bad := append([]byte(nil), png[:33]...)
	bad = append(bad, 255, 255, 255, 255, 't', 'E', 'X', 't')
	bad = append(bad, png[33:]...)
	expect(t, b.upload("/assets/1", url.Values{"revision": {"1"}, "description": {"Privacy test"}}, bad), 400)
	current, _ := app.store.Get(1)
	if current.Revision != 1 || len(current.Photos) != 1 {
		t.Fatal("failed upload changed entry")
	}
	for _, folder := range []string{"photos", "thumbnails"} {
		files, _ := os.ReadDir(filepath.Join(dir, folder))
		if len(files) != 1 {
			t.Fatal("failed upload leaked files")
		}
	}
}

func TestStripMetadataRejectsTruncatedStructures(t *testing.T) {
	for _, tc := range []struct {
		format string
		data   []byte
	}{{"jpeg", []byte{0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff}}, {"jpeg", []byte{0xff, 0xd8, 0xff}}, {"png", append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, 1, 2, 3)}, {"gif", []byte("GIF89a")}} {
		if _, err := stripPhotoMetadata(tc.data, tc.format); err == nil {
			t.Fatal("accepted malformed metadata", tc.format)
		}
	}
}

// Small progressive JPEG fixture, encoded once with Pillow. Tests do not require it.
func TestStripProgressiveJPEGMetadata(t *testing.T) {
	const fixture = "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wgARCAADAAUDASIAAhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAX/xAAVAQEBAAAAAAAAAAAAAAAAAAADBP/aAAwDAQACEAMQAAABmgIP/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABBQJ//8QAFBEBAAAAAAAAAAAAAAAAAAAAAP/aAAgBAwEBPwF//8QAFBEBAAAAAAAAAAAAAAAAAAAAAP/aAAgBAgEBPwF//8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQAGPwJ//8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPyF//9oADAMBAAIAAwAAABD7/8QAFBEBAAAAAAAAAAAAAAAAAAAAAP/aAAgBAwEBPxB//8QAFBEBAAAAAAAAAAAAAAAAAAAAAP/aAAgBAgEBPxB//8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxB//9k="
	original, err := base64.StdEncoding.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}
	scan := bytes.Index(original, []byte{0xff, 0xda})
	next := scan + bytes.Index(original[scan:], []byte{0xff, 0xc4})
	raw := append([]byte(nil), original[:next]...)
	raw = append(raw, jpegSegment(0xed, []byte("PRIVATE GPS COORDINATES"))...)
	raw = append(raw, original[next:]...)
	cleaned, err := stripPhotoMetadata(raw, "jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cleaned, original) {
		t.Fatal("progressive JPEG scan data changed")
	}
	if _, err := jpeg.Decode(bytes.NewReader(cleaned)); err != nil {
		t.Fatal(err)
	}
}
