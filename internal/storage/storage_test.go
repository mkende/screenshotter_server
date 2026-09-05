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
	err := s.Save("testid", strings.NewReader("this is not a png"))
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

	if err := s.Save(id, bytes.NewReader(data)); err != nil {
		t.Fatalf("Save: %v", err)
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

	if err := s.Save(id, bytes.NewReader(data)); err != nil {
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
	err := s.Save("testid", bytes.NewReader(data))
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

// corruptPNG returns bytes that pass the signature and IHDR dimension checks
// but fail to decode (garbage chunk data).
func corruptPNG(w, h uint32) []byte {
	data := make([]byte, 64)
	copy(data, pngMagic)
	binary.BigEndian.PutUint32(data[ihdrWidthOffset:], w)
	binary.BigEndian.PutUint32(data[ihdrWidthOffset+4:], h)
	return data
}

// tempFiles returns the names of any in-progress temp files left under the
// storage root (image and thumbs directories).
func tempFiles(t *testing.T, s *Storage) []string {
	t.Helper()
	var found []string
	for _, dir := range []string{s.base, filepath.Join(s.base, "thumbs")} {
		matches, err := filepath.Glob(filepath.Join(dir, tempPattern))
		if err != nil {
			t.Fatalf("glob: %v", err)
		}
		found = append(found, matches...)
	}
	return found
}

// TestSave_UndecodablePNG_KeepsExistingFiles verifies that replacing an image
// with a PNG that passes the header checks but fails to decode leaves the
// previous image and thumbnail untouched (regression: the original was deleted).
func TestSave_UndecodablePNG_KeepsExistingFiles(t *testing.T) {
	s := newTestStorage(t)
	id := "keep1234"
	original := makePNG(t, 40, 30)
	if err := s.SaveNew(id, original); err != nil {
		t.Fatalf("SaveNew: %v", err)
	}
	origThumb, err := os.ReadFile(s.ThumbPath(id))
	if err != nil {
		t.Fatalf("read thumb: %v", err)
	}

	err = s.Save(id, bytes.NewReader(corruptPNG(40, 30)))
	if err == nil {
		t.Fatal("expected Save to fail for undecodable PNG")
	}
	if errors.Is(err, ErrNotPNG) || errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("expected a decode error, got %v", err)
	}

	got, err := os.ReadFile(s.ImagePath(id))
	if err != nil {
		t.Fatalf("original image must still exist: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Error("original image content was modified by the failed Save")
	}
	gotThumb, err := os.ReadFile(s.ThumbPath(id))
	if err != nil {
		t.Fatalf("original thumb must still exist: %v", err)
	}
	if !bytes.Equal(gotThumb, origThumb) {
		t.Error("original thumbnail was modified by the failed Save")
	}
	if left := tempFiles(t, s); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// TestSave_ReplacesImageAndThumbnail verifies a successful Save over an
// existing image swaps both files and leaves no temp files.
func TestSave_ReplacesImageAndThumbnail(t *testing.T) {
	s := newTestStorage(t)
	id := "repl1234"
	if err := s.SaveNew(id, makePNG(t, 40, 30)); err != nil {
		t.Fatalf("SaveNew: %v", err)
	}
	oldThumb, _ := os.ReadFile(s.ThumbPath(id))

	replacement := makePNG(t, 800, 20)
	if err := s.Save(id, bytes.NewReader(replacement)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(s.ImagePath(id))
	if err != nil {
		t.Fatalf("read image: %v", err)
	}
	if !bytes.Equal(got, replacement) {
		t.Error("image content was not replaced")
	}
	newThumb, err := os.ReadFile(s.ThumbPath(id))
	if err != nil {
		t.Fatalf("read thumb: %v", err)
	}
	if bytes.Equal(newThumb, oldThumb) {
		t.Error("thumbnail was not regenerated")
	}
	if left := tempFiles(t, s); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
	if info, err := os.Stat(s.ImagePath(id)); err == nil && info.Mode().Perm() != 0o640 {
		t.Errorf("image mode = %o, want 640", info.Mode().Perm())
	}
}

// TestSaveNew_UndecodablePNG_LeavesNothingBehind verifies a failed first save
// writes neither an image nor a thumbnail nor any temp file.
func TestSaveNew_UndecodablePNG_LeavesNothingBehind(t *testing.T) {
	s := newTestStorage(t)
	id := "none1234"
	if err := s.SaveNew(id, corruptPNG(10, 10)); err == nil {
		t.Fatal("expected SaveNew to fail")
	}
	if _, err := os.Stat(s.ImagePath(id)); !os.IsNotExist(err) {
		t.Errorf("image file should not exist, stat err = %v", err)
	}
	if _, err := os.Stat(s.ThumbPath(id)); !os.IsNotExist(err) {
		t.Errorf("thumb file should not exist, stat err = %v", err)
	}
	if left := tempFiles(t, s); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// TestSaveNew_ExistingFile_ReturnsIDCollision verifies the corrupt-state guard.
func TestSaveNew_ExistingFile_ReturnsIDCollision(t *testing.T) {
	s := newTestStorage(t)
	id := "coll1234"
	original := makePNG(t, 10, 10)
	if err := s.SaveNew(id, original); err != nil {
		t.Fatalf("SaveNew: %v", err)
	}
	err := s.SaveNew(id, makePNG(t, 20, 20))
	if !errors.Is(err, ErrIDCollision) {
		t.Fatalf("expected ErrIDCollision, got %v", err)
	}
	got, _ := os.ReadFile(s.ImagePath(id))
	if !bytes.Equal(got, original) {
		t.Error("existing image was overwritten despite collision")
	}
}

// TestNew_RemovesStaleTempFiles verifies leftover temp files from a crashed
// write are cleaned up at startup, while real images are kept.
func TestNew_RemovesStaleTempFiles(t *testing.T) {
	base := t.TempDir()
	thumbs := filepath.Join(base, "thumbs")
	if err := os.MkdirAll(thumbs, 0o750); err != nil {
		t.Fatal(err)
	}
	stale := []string{
		filepath.Join(base, ".abc12345.123.tmp"),
		filepath.Join(thumbs, ".abc12345.456.tmp"),
	}
	keep := filepath.Join(base, "abc12345.png")
	for _, p := range append(stale, keep) {
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := New(base); err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, p := range stale {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("stale temp %s should be removed, stat err = %v", p, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("real image should be kept: %v", err)
	}
}
