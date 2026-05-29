// Package storage handles saving, serving, and deleting image files on disk.
// Images are stored as <base>/<id>.png and thumbnails as <base>/thumbs/<id>.png.
package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/image/draw"
)

const (
	thumbMaxDim    = 320        // longest side of the generated thumbnail in pixels
	maxImageDim    = 20_000     // maximum width or height accepted before decoding
	maxImagePixels = 40_000_000 // maximum total pixel count (40 Mpx — covers 8K screens)

	// PNG structure: 8-byte magic + 4-byte IHDR length + 4-byte "IHDR" type,
	// then 4-byte width + 4-byte height. ihdrWidthOffset is the byte index of width.
	ihdrWidthOffset = 16
	ihdrMinBytes    = 24 // minimum bytes needed to read IHDR dimensions
)

// pngMagic is the 8-byte PNG file signature.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// decodeSem bounds the number of PNG decodes (and thumbnail scaling) running
// concurrently. A single 40 Mpx image expands to ~160 MB of RGBA while being
// decoded and scaled; without this cap a burst of large uploads could exhaust
// memory even though each one passes the per-image dimension check.
var decodeSem = make(chan struct{}, maxConcurrentDecodes())

// maxConcurrentDecodes returns the decode concurrency limit: GOMAXPROCS capped
// at 4, and at least 1.
func maxConcurrentDecodes() int {
	n := runtime.GOMAXPROCS(0)
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

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

// ValidatePNG checks that data has a valid PNG header and is within dimension
// limits. Returns ErrNotPNG or ErrImageTooLarge on failure.
func ValidatePNG(data []byte) error {
	if !isPNG(data) {
		return ErrNotPNG
	}
	return checkPNGDimensions(data)
}

// SaveNew writes a new image for id using O_EXCL (fails if the file already
// exists) and generates a thumbnail. data must be pre-validated with
// ValidatePNG. Returns ErrIDCollision if the file already exists on disk.
func (s *Storage) SaveNew(id string, data []byte) error {
	imgPath := s.imagePath(id)
	f, err := os.OpenFile(imgPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		if os.IsExist(err) {
			return ErrIDCollision
		}
		return fmt.Errorf("write image: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(imgPath)
		return fmt.Errorf("write image: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(imgPath)
		return fmt.Errorf("write image: %w", err)
	}
	if err := s.generateThumb(id, data); err != nil {
		os.Remove(imgPath)
		return fmt.Errorf("generate thumbnail: %w", err)
	}
	return nil
}

// Save reads all of r, validates the PNG, overwrites any existing file, and
// regenerates the thumbnail. Used by the annotation path to replace images.
func (s *Storage) Save(id string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read upload: %w", err)
	}
	if !isPNG(data) {
		return ErrNotPNG
	}
	if err := checkPNGDimensions(data); err != nil {
		return err
	}
	imgPath := s.imagePath(id)
	if err := os.WriteFile(imgPath, data, 0o640); err != nil {
		return fmt.Errorf("write image: %w", err)
	}
	if err := s.generateThumb(id, data); err != nil {
		os.Remove(imgPath)
		return fmt.Errorf("generate thumbnail: %w", err)
	}
	return nil
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

// checkPNGDimensions reads the IHDR width and height from data and returns
// ErrImageTooLarge if either dimension exceeds maxImageDim or the total pixel
// count exceeds maxImagePixels. This is called before png.Decode to prevent
// decompression-bomb DoS on crafted inputs.
func checkPNGDimensions(data []byte) error {
	if len(data) < ihdrMinBytes {
		return ErrNotPNG
	}
	w := binary.BigEndian.Uint32(data[ihdrWidthOffset:])
	h := binary.BigEndian.Uint32(data[ihdrWidthOffset+4:])
	if w > maxImageDim || h > maxImageDim || uint64(w)*uint64(h) > maxImagePixels {
		return ErrImageTooLarge
	}
	return nil
}

func (s *Storage) generateThumb(id string, data []byte) error {
	// Serialise the memory-heavy decode + scale behind decodeSem so concurrent
	// large uploads cannot collectively exhaust memory.
	decodeSem <- struct{}{}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		<-decodeSem
		return fmt.Errorf("decode png: %w", err)
	}
	thumb := scaleDown(src, thumbMaxDim)
	<-decodeSem
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

// ErrImageTooLarge is returned when the PNG dimensions exceed the safe limit.
var ErrImageTooLarge = fmt.Errorf("image dimensions exceed the allowed maximum (%dx%d px or %d Mpx total)", maxImageDim, maxImageDim, maxImagePixels/1_000_000)

// ErrIDCollision is returned by SaveNew when a file for the given ID already
// exists on disk, indicating a corrupt state (file without a DB record).
var ErrIDCollision = fmt.Errorf("image file already exists for this ID")
