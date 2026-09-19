package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// icoMagic starts every ICO file (reserved 0, type 1 = icon).
var icoMagic = []byte{0, 0, 1, 0}

func TestEmbeddedFaviconHandler_ServesICO(t *testing.T) {
	rr := httptest.NewRecorder()
	embeddedFaviconHandler(rr, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/x-icon" {
		t.Errorf("expected image/x-icon, got %q", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != faviconCacheControl {
		t.Errorf("expected Cache-Control %q, got %q", faviconCacheControl, cc)
	}
	if !bytes.HasPrefix(rr.Body.Bytes(), icoMagic) {
		t.Errorf("body does not start with the ICO header: % x", rr.Body.Bytes()[:4])
	}
}

func TestFaviconHandler_ServesConfiguredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favicon.ico")
	content := append(append([]byte{}, icoMagic...), 'x')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	faviconHandler(path)(rr, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !bytes.Equal(rr.Body.Bytes(), content) {
		t.Errorf("body differs from the configured file")
	}
	if cc := rr.Header().Get("Cache-Control"); cc != faviconCacheControl {
		t.Errorf("expected Cache-Control %q, got %q", faviconCacheControl, cc)
	}

	rr = httptest.NewRecorder()
	faviconHandler(filepath.Join(t.TempDir(), "missing.ico"))(rr, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for a missing file, got %d", rr.Code)
	}
}
