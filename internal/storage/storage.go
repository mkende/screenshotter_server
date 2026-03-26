// Package storage handles saving, serving, and deleting image files on disk.
// Images are stored as <base>/<id>.png and thumbnails as <base>/thumbs/<id>.png.
package storage

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

const (
	thumbMaxDim = 320 // longest side of the generated thumbnail in pixels
)

// pngMagic is the 8-byte PNG file signature.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// Storage manages image files under a base directory.
type Storage struct {
	base string
}

// New creates a Storage rooted at base, creating required directories.
func New(base string) (*Storage, error) {
	for _, dir := range []string{base, filepath.Join(base, "thumbs")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create storage dir %q: %w", dir, err)
		}
	}
	return &Storage{base: base}, nil
}

// Save reads all of r, validates the PNG magic bytes, writes the file to disk,
// and generates a thumbnail. It returns the relative file path stored in the DB.
func (s *Storage) Save(id string, r io.Reader) (filePath string, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read upload: %w", err)
	}
	if !isPNG(data) {
		return "", ErrNotPNG
	}
	imgPath := s.imagePath(id)
	if err := os.WriteFile(imgPath, data, 0o640); err != nil {
		return "", fmt.Errorf("write image: %w", err)
	}
	// Generate thumbnail; ignore errors so a thumb failure doesn't fail the upload.
	if err := s.generateThumb(id, data); err != nil {
		// Best-effort: remove main file so the upload is fully rolled back.
		os.Remove(imgPath)
		return "", fmt.Errorf("generate thumbnail: %w", err)
	}
	return id + ".png", nil
}

// Delete removes the image and its thumbnail from disk.
func (s *Storage) Delete(id string) error {
	if err := os.Remove(s.imagePath(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete image: %w", err)
	}
	if err := os.Remove(s.thumbPath(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete thumbnail: %w", err)
	}
	return nil
}

// ImagePath returns the absolute path to an image file.
func (s *Storage) ImagePath(id string) string { return s.imagePath(id) }

// ThumbPath returns the absolute path to a thumbnail file.
func (s *Storage) ThumbPath(id string) string { return s.thumbPath(id) }

func (s *Storage) imagePath(id string) string {
	return filepath.Join(s.base, id+".png")
}

func (s *Storage) thumbPath(id string) string {
	return filepath.Join(s.base, "thumbs", id+".png")
}

func isPNG(data []byte) bool {
	return len(data) >= len(pngMagic) && bytes.Equal(data[:len(pngMagic)], pngMagic)
}

func (s *Storage) generateThumb(id string, data []byte) error {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode png: %w", err)
	}
	thumb := scaleDown(src, thumbMaxDim)
	f, err := os.OpenFile(s.thumbPath(id), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("create thumb file: %w", err)
	}
	if encErr := png.Encode(f, thumb); encErr != nil {
		f.Close()
		os.Remove(s.thumbPath(id))
		return fmt.Errorf("encode thumb: %w", encErr)
	}
	return f.Close()
}

// scaleDown returns src scaled so its longest dimension is at most maxDim,
// preserving aspect ratio. Returns src unchanged if it already fits.
func scaleDown(src image.Image, maxDim int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		return src
	}
	var tw, th int
	if w >= h {
		tw = maxDim
		th = (h * maxDim) / w
	} else {
		th = maxDim
		tw = (w * maxDim) / h
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

// ErrNotPNG is returned when the uploaded file does not have a valid PNG header.
var ErrNotPNG = fmt.Errorf("uploaded file is not a valid PNG")
