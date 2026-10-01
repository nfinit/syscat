package catalog

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var errPhotoMetadata = errors.New("unable to safely remove image metadata; upload a valid JPEG, PNG, or GIF file")

// Strip location-capable metadata without recompressing image data. Keep only
// display-related metadata (including minimal JPEG EXIF orientation), and discard
// trailing data after the image terminator. Do not retain opaque vendor blocks.
func stripPhotoMetadata(data []byte, format string) ([]byte, error) {
	switch format {
	case "jpeg":
		return stripJPEGMetadata(data)
	case "png":
		return stripPNGMetadata(data)
	case "gif":
		return stripGIFMetadata(data)
	default:
		return nil, errPhotoMetadata
	}
}

func exifOrientation(data []byte) uint16 {
	if len(data) < 14 || !bytes.HasPrefix(data, []byte("Exif\x00\x00")) {
		return 0
	}
	tiff := data[6:]
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0
	}
	offset := uint64(order.Uint32(tiff[4:8]))
	if offset < 8 || offset+2 > uint64(len(tiff)) {
		return 0
	}
	count := uint64(order.Uint16(tiff[offset : offset+2]))
	if offset+2+count*12+4 > uint64(len(tiff)) {
		return 0
	}
	for i := uint64(0); i < count; i++ {
		entry := tiff[offset+2+i*12 : offset+2+(i+1)*12]
		if order.Uint16(entry[:2]) == 0x112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
			orientation := order.Uint16(entry[8:10])
			if orientation >= 1 && orientation <= 8 {
				return orientation
			}
		}
	}
	return 0
}

func orientationEXIF(orientation uint16) []byte {
	return []byte{0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, byte(orientation), 0, 0, 0, 0, 0, 0, 0}
}

func stripJPEGMetadata(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, errPhotoMetadata
	}
	out := append([]byte(nil), data[:2]...)
	var orientation uint16
	for pos := 2; pos < len(data); {
		start := pos
		if data[pos] != 0xff {
			return nil, errPhotoMetadata
		}
		for pos < len(data) && data[pos] == 0xff {
			pos++
		}
		if pos >= len(data) {
			return nil, errPhotoMetadata
		}
		marker := data[pos]
		pos++
		if marker == 0xd9 {
			if orientation != 0 {
				result := append([]byte(nil), out[:2]...)
				result = append(result, orientationEXIF(orientation)...)
				out = append(result, out[2:]...)
			}
			return append(out, 0xff, 0xd9), nil
		}
		if marker == 0 || marker == 0xd8 {
			return nil, errPhotoMetadata
		}
		if marker == 1 || marker >= 0xd0 && marker <= 0xd7 {
			out = append(out, data[start:pos]...)
			continue
		}
		if pos+2 > len(data) {
			return nil, errPhotoMetadata
		}
		length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if length < 2 || length > len(data)-pos {
			return nil, errPhotoMetadata
		}
		payload := data[pos+2 : pos+length]
		keep := marker != 0xfe && !(marker >= 0xe0 && marker <= 0xef)
		switch marker {
		case 0xe0:
			keep = bytes.HasPrefix(payload, []byte("JFIF\x00"))
		case 0xe1:
			if orientation == 0 {
				orientation = exifOrientation(payload)
			}
		case 0xe2:
			keep = bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00"))
		case 0xee:
			keep = bytes.HasPrefix(payload, []byte("Adobe"))
		}
		pos += length
		if keep {
			out = append(out, data[start:pos]...)
		}
		if marker == 0xda {
			// Entropy-coded scan bytes contain stuffed FF00 and restart markers. Stop
			// at the next real marker so metadata between progressive scans is stripped.
			start = pos
			for pos < len(data) {
				if data[pos] != 0xff {
					pos++
					continue
				}
				markerStart := pos
				for pos < len(data) && data[pos] == 0xff {
					pos++
				}
				if pos >= len(data) {
					return nil, errPhotoMetadata
				}
				code := data[pos]
				if code == 0 || code >= 0xd0 && code <= 0xd7 {
					pos++
					continue
				}
				pos = markerStart
				break
			}
			out = append(out, data[start:pos]...)
		}
	}
	return nil, errPhotoMetadata
}

func stripPNGMetadata(data []byte) ([]byte, error) {
	signature := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	if !bytes.HasPrefix(data, signature) {
		return nil, errPhotoMetadata
	}
	out := append([]byte(nil), signature...)
	for pos := 8; pos < len(data); {
		if len(data)-pos < 12 {
			return nil, errPhotoMetadata
		}
		length := uint64(binary.BigEndian.Uint32(data[pos : pos+4]))
		if length+12 > uint64(len(data)-pos) {
			return nil, errPhotoMetadata
		}
		end := pos + int(length) + 12
		kind := string(data[pos+4 : pos+8])
		// All critical chunks plus known color/transparency/display and APNG chunks.
		keep := data[pos+4]&0x20 == 0
		switch kind {
		case "cHRM", "gAMA", "iCCP", "sRGB", "sBIT", "tRNS", "bKGD", "hIST", "pHYs", "acTL", "fcTL", "fdAT":
			keep = true
		}
		if keep {
			out = append(out, data[pos:end]...)
		}
		if kind == "IEND" {
			return out, nil
		}
		pos = end
	}
	return nil, errPhotoMetadata
}

func gifBlocksEnd(data []byte, pos int) (int, error) {
	for pos < len(data) {
		size := int(data[pos])
		pos++
		if size == 0 {
			return pos, nil
		}
		if size > len(data)-pos {
			return 0, errPhotoMetadata
		}
		pos += size
	}
	return 0, errPhotoMetadata
}

func stripGIFMetadata(data []byte) ([]byte, error) {
	if len(data) < 13 || string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a" {
		return nil, errPhotoMetadata
	}
	pos := 13
	if data[10]&0x80 != 0 {
		pos += 3 * (1 << ((data[10] & 7) + 1))
	}
	if pos > len(data) {
		return nil, errPhotoMetadata
	}
	out := append([]byte(nil), data[:pos]...)
	for pos < len(data) {
		start := pos
		switch data[pos] {
		case 0x3b:
			return append(out, 0x3b), nil
		case 0x2c:
			if len(data)-pos < 10 {
				return nil, errPhotoMetadata
			}
			packed := data[pos+9]
			pos += 10
			if packed&0x80 != 0 {
				pos += 3 * (1 << ((packed & 7) + 1))
			}
			if pos >= len(data) {
				return nil, errPhotoMetadata
			}
			end, err := gifBlocksEnd(data, pos+1)
			if err != nil {
				return nil, err
			}
			pos = end
			out = append(out, data[start:pos]...)
		case 0x21:
			if len(data)-pos < 3 {
				return nil, errPhotoMetadata
			}
			label := data[pos+1]
			end, err := gifBlocksEnd(data, pos+2)
			if err != nil {
				return nil, err
			}
			keep := label == 0xf9 // Frame delay, transparency, and disposal.
			if label == 0xff && data[pos+2] == 11 && end-pos == 19 {
				app := string(data[pos+3 : pos+14])
				keep = (app == "NETSCAPE2.0" || app == "ANIMEXTS1.0") && data[pos+14] == 3 && data[pos+15] == 1 && data[pos+18] == 0
			}
			pos = end
			if keep {
				out = append(out, data[start:pos]...)
			}
		default:
			return nil, errPhotoMetadata
		}
	}
	return nil, errPhotoMetadata
}
