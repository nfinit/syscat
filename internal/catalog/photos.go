package catalog

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
)

const maxPhotoBytes = 12 << 20

func randomKey() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func savePhoto(dir string, header *multipart.FileHeader) (Photo, error) {
	var p Photo
	if header.Size > maxPhotoBytes {
		return p, errors.New("each photo must be 12 MB or smaller")
	}
	src, err := header.Open()
	if err != nil {
		return p, err
	}
	defer src.Close()
	cfg, format, err := image.DecodeConfig(src)
	if err != nil {
		return p, errors.New("unsupported or invalid image; accepted formats: JPEG, PNG, GIF")
	}
	extension := map[string]string{"jpeg": ".jpg", "png": ".png", "gif": ".gif"}[format]
	if extension == "" {
		return p, errors.New("unsupported image format; accepted formats: JPEG, PNG, GIF")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32_000_000 {
		return p, errors.New("photos must be no larger than 32 megapixels")
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return p, err
	}
	img, err := imaging.Decode(src, imaging.AutoOrientation(true))
	if err != nil {
		return p, errors.New("unable to decode image; upload a valid JPEG, PNG, or GIF file")
	}
	key := randomKey()
	p = Photo{Path: "photos/" + key + extension, Thumbnail: "thumbnails/" + key + ".jpg", Name: filepath.Base(header.Filename)}
	ok := false
	defer func() {
		if !ok {
			removePhotos(dir, []Photo{p})
		}
	}()
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return Photo{}, err
	}
	file, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(p.Path)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Photo{}, err
	}
	_, copyErr := io.Copy(file, io.LimitReader(src, maxPhotoBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return Photo{}, copyErr
	}
	if closeErr != nil {
		return Photo{}, closeErr
	}
	thumb := imaging.Fit(img, 1000, 1000, imaging.Lanczos)
	if err := imaging.Save(thumb, filepath.Join(dir, filepath.FromSlash(p.Thumbnail)), imaging.JPEGQuality(85)); err != nil {
		return Photo{}, err
	}
	ok = true
	return p, nil
}

func saveUploads(dir string, form *multipart.Form) ([]Photo, error) {
	photos := []Photo{}
	if form == nil {
		return photos, nil
	}
	files := form.File["photos"]
	for _, header := range files {
		p, err := savePhoto(dir, header)
		if err != nil {
			removePhotos(dir, photos)
			return nil, fmt.Errorf("%s: %w", header.Filename, err)
		}
		photos = append(photos, p)
	}
	return photos, nil
}

func removePhotos(dir string, photos []Photo) {
	for _, p := range photos {
		_ = os.Remove(filepath.Join(dir, filepath.FromSlash(p.Path)))
		_ = os.Remove(filepath.Join(dir, filepath.FromSlash(p.Thumbnail)))
	}
}
