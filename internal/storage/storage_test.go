package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makePNG returns a valid in-memory PNG with the given dimensions.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fill with a solid colour so it has content.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// TestIsPNG verifies the magic-byte check.
func TestIsPNG(t *testing.T) {
	t.Run("valid PNG bytes", func(t *testing.T) {
		data := makePNG(t, 10, 10)
		if !isPNG(data) {
			t.Error("expected isPNG to return true for valid PNG bytes")
		}
	})

	t.Run("JPEG bytes rejected", func(t *testing.T) {
		// JPEG starts with 0xFF 0xD8
		data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}
		if isPNG(data) {
			t.Error("expected isPNG to return false for JPEG bytes")
		}
	})

	t.Run("empty bytes rejected", func(t *testing.T) {
		if isPNG([]byte{}) {
			t.Error("expected isPNG to return false for empty bytes")
		}
	})

	t.Run("too short rejected", func(t *testing.T) {
		if isPNG(pngMagic[:4]) {
			t.Error("expected isPNG to return false for partial magic bytes")
		}
	})
}

// TestSave_RejectsNonPNG ensures Save returns ErrNotPNG for non-PNG data.
func TestSave_RejectsNonPNG(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.Save("testid", strings.NewReader("this is not a png"))
	if err == nil {
		t.Fatal("expected ErrNotPNG, got nil")
	}
	if err != ErrNotPNG {
		t.Errorf("expected ErrNotPNG, got: %v", err)
	}
}

// TestSave_WritesFileAndThumbnail verifies that after a successful Save,
// both the image file and its thumbnail exist on disk.
func TestSave_WritesFileAndThumbnail(t *testing.T) {
	s := newTestStorage(t)
	data := makePNG(t, 100, 80)
	id := "abc12345"

	filePath, err := s.Save(id, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filePath != id+".png" {
		t.Errorf("unexpected filePath %q", filePath)
	}

	// Main image must exist.
	if _, err := os.Stat(s.ImagePath(id)); err != nil {
		t.Errorf("image file missing: %v", err)
	}
	// Thumbnail must exist.
	if _, err := os.Stat(s.ThumbPath(id)); err != nil {
		t.Errorf("thumb file missing: %v", err)
	}
}

// TestDelete_RemovesBothFiles verifies that Delete removes both the image and thumbnail.
func TestDelete_RemovesBothFiles(t *testing.T) {
	s := newTestStorage(t)
	data := makePNG(t, 50, 50)
	id := "deltest1"

	if _, err := s.Save(id, bytes.NewReader(data)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(s.ImagePath(id)); !os.IsNotExist(err) {
		t.Error("image file should have been deleted")
	}
	if _, err := os.Stat(s.ThumbPath(id)); !os.IsNotExist(err) {
		t.Error("thumb file should have been deleted")
	}
}

// TestDelete_NonExistentIsOK verifies that Delete on a missing file is not an error.
func TestDelete_NonExistentIsOK(t *testing.T) {
	s := newTestStorage(t)
	if err := s.Delete("doesnotexist"); err != nil {
		t.Errorf("Delete on missing file should not error: %v", err)
	}
}

// TestScaleDown_PreservesAspectRatioAndDoesNotUpscale tests the scaleDown helper.
func TestScaleDown(t *testing.T) {
	t.Run("wide image scaled down preserving aspect ratio", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 640, 320))
		result := scaleDown(src, 320)
		b := result.Bounds()
		if b.Dx() != 320 {
			t.Errorf("expected width 320, got %d", b.Dx())
		}
		if b.Dy() != 160 {
			t.Errorf("expected height 160, got %d", b.Dy())
		}
	})

	t.Run("tall image scaled down preserving aspect ratio", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 200, 800))
		result := scaleDown(src, 320)
		b := result.Bounds()
		if b.Dy() != 320 {
			t.Errorf("expected height 320, got %d", b.Dy())
		}
		if b.Dx() != 80 {
			t.Errorf("expected width 80, got %d", b.Dx())
		}
	})

	t.Run("small image not upscaled", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 100, 50))
		result := scaleDown(src, 320)
		b := result.Bounds()
		if b.Dx() != 100 || b.Dy() != 50 {
			t.Errorf("small image should not be upscaled, got %dx%d", b.Dx(), b.Dy())
		}
		// Should return the same object (not a new scaled image).
		if result != src {
			t.Error("expected the same image to be returned unchanged")
		}
	})

	t.Run("square image scaled to maxDim x maxDim", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 1000, 1000))
		result := scaleDown(src, 320)
		b := result.Bounds()
		if b.Dx() != 320 || b.Dy() != 320 {
			t.Errorf("expected 320x320, got %dx%d", b.Dx(), b.Dy())
		}
	})
}

// makePNGHeader returns exactly ihdrMinBytes bytes containing a valid PNG magic
// and IHDR chunk header with the given dimensions, but no actual image data.
// This lets tests exercise checkPNGDimensions without allocating large images.
func makePNGHeader(w, h uint32) []byte {
	buf := make([]byte, ihdrMinBytes)
	copy(buf, pngMagic)
	// IHDR chunk: 4-byte length (13), 4-byte type "IHDR", 4-byte width, 4-byte height.
	binary.BigEndian.PutUint32(buf[8:], 13)
	copy(buf[12:], []byte("IHDR"))
	binary.BigEndian.PutUint32(buf[16:], w)
	binary.BigEndian.PutUint32(buf[20:], h)
	return buf
}

// TestCheckPNGDimensions exercises the dimension guard against decompression bombs.
func TestCheckPNGDimensions(t *testing.T) {
	t.Run("normal dimensions accepted", func(t *testing.T) {
		if err := checkPNGDimensions(makePNGHeader(1920, 1080)); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})
	t.Run("at exact limit accepted", func(t *testing.T) {
		if err := checkPNGDimensions(makePNGHeader(maxImageDim, 1)); err != nil {
			t.Errorf("expected no error at limit, got: %v", err)
		}
	})
	t.Run("width over limit rejected", func(t *testing.T) {
		err := checkPNGDimensions(makePNGHeader(maxImageDim+1, 100))
		if !errors.Is(err, ErrImageTooLarge) {
			t.Errorf("expected ErrImageTooLarge, got: %v", err)
		}
	})
	t.Run("height over limit rejected", func(t *testing.T) {
		err := checkPNGDimensions(makePNGHeader(100, maxImageDim+1))
		if !errors.Is(err, ErrImageTooLarge) {
			t.Errorf("expected ErrImageTooLarge, got: %v", err)
		}
	})
	t.Run("pixel count over limit rejected", func(t *testing.T) {
		// 10000×6000 = 60 Mpx > maxImagePixels but each dim < maxImageDim.
		err := checkPNGDimensions(makePNGHeader(10_000, 6_000))
		if !errors.Is(err, ErrImageTooLarge) {
			t.Errorf("expected ErrImageTooLarge for 60 Mpx image, got: %v", err)
		}
	})
	t.Run("too short data returns ErrNotPNG", func(t *testing.T) {
		err := checkPNGDimensions(makePNGHeader(100, 100)[:10])
		if !errors.Is(err, ErrNotPNG) {
			t.Errorf("expected ErrNotPNG for truncated header, got: %v", err)
		}
	})
}

// TestSave_RejectsOversizedDimensions ensures Save returns ErrImageTooLarge for
// a PNG whose IHDR declares dimensions beyond the allowed maximum.
func TestSave_RejectsOversizedDimensions(t *testing.T) {
	// Build a real tiny PNG and patch its IHDR width to a forbidden value.
	data := makePNG(t, 10, 10)
	binary.BigEndian.PutUint32(data[ihdrWidthOffset:], maxImageDim+1)
	// The CRC will be invalid, but checkPNGDimensions fires before png.Decode.

	s := newTestStorage(t)
	_, err := s.Save("testid", bytes.NewReader(data))
	if !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("expected ErrImageTooLarge, got: %v", err)
	}
}

// TestNew_CreatesDirectories confirms that New creates the base and thumbs dirs.
func TestNew_CreatesDirectories(t *testing.T) {
	base := filepath.Join(t.TempDir(), "storage")
	_, err := New(base)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(base); err != nil {
		t.Errorf("base dir not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "thumbs")); err != nil {
		t.Errorf("thumbs dir not created: %v", err)
	}
}
