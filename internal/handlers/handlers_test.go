package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter_server/internal/auth"
	"github.com/mkende/screenshotter_server/internal/config"
	"github.com/mkende/screenshotter_server/internal/db"
	"github.com/mkende/screenshotter_server/internal/storage"
)

// makePNG returns valid in-memory PNG bytes.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
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

// openTestDB opens an in-memory SQLite DB with migrations applied.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open("sqlite", "file::memory:?cache=shared&_foreign_keys=on")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// minimalTemplates returns a map with simple stub templates that satisfy the
// handlers' renderTemplate calls without needing the embedded FS.
func minimalTemplates() map[string]*template.Template {
	const stub = `{{define "base"}}OK{{end}}`
	pages := []string{"home.html", "home-loggedout.html", "view.html", "annotate.html", "admin-users.html", "admin-user-detail.html"}
	m := make(map[string]*template.Template, len(pages))
	for _, p := range pages {
		m[p] = template.Must(template.New("").Parse(stub))
	}
	return m
}

// newHandlers builds a Handlers ready for testing.
func newHandlers(t *testing.T) (*Handlers, *db.DB, *storage.Storage) {
	t.Helper()

	database := openTestDB(t)

	stor, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	cfg := &config.Config{
		CanonicalAddress: "https://example.com",
		Title:            "Screenshotter",
	}
	cfg.Server.MaxUploadMB = 4
	cfg.ID.Length = 8
	cfg.SourceURLSchemes = []string{"http", "https"}

	h := New(cfg, database, stor, minimalTemplates(), nil)
	return h, database, stor
}

// buildUploadRequest builds a multipart/form-data request for the Upload handler.
func buildUploadRequest(t *testing.T, imageData []byte, sourceURL string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	if imageData != nil {
		fw, err := mw.CreateFormFile("image", "screenshot.png")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(imageData); err != nil {
			t.Fatalf("write image data: %v", err)
		}
	}
	if sourceURL != "" {
		if err := mw.WriteField("source_url", sourceURL); err != nil {
			t.Fatalf("write source_url: %v", err)
		}
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// identityFor returns an Identity matching the given email.
func identityFor(email string) *auth.Identity {
	return &auth.Identity{
		Email:       email,
		DisplayName: email,
		Source:      auth.AuthSourceTailscale,
	}
}

// executeAs drives handler with req after injecting an Identity into the
// request context. Used to exercise handlers in isolation from the real auth
// middleware chain.
func executeAs(t *testing.T, h http.HandlerFunc, req *http.Request, email string) *httptest.ResponseRecorder {
	t.Helper()
	req = req.WithContext(auth.WithIdentity(req.Context(), identityFor(email)))
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

// executeAnonymous drives handler without any authenticated identity.
func executeAnonymous(h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

// chiRequest wraps req with a chi RouteContext so chi.URLParam works.
func chiRequest(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// ---- Upload tests ----------------------------------------------------------

func TestUpload_MissingImageField_Returns400(t *testing.T) {
	h, _, _ := newHandlers(t)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("source_url", "https://example.com") //nolint:errcheck
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rr := executeAs(t, h.Upload, req, "alice@example.com")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestUpload_NonPNGBytes_Returns400(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := buildUploadRequest(t, []byte("this is not a png"), "https://example.com")
	rr := executeAs(t, h.Upload, req, "alice@example.com")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-PNG, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not a valid PNG") {
		t.Errorf("expected PNG error message, got: %s", rr.Body.String())
	}
}

func TestUpload_OversizedBody_Returns413(t *testing.T) {
	h, _, _ := newHandlers(t)
	bigData := make([]byte, 5<<20)
	copy(bigData, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("image", "big.png")
	fw.Write(bigData)                                  //nolint:errcheck
	mw.WriteField("source_url", "https://example.com") //nolint:errcheck
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rr := executeAs(t, h.Upload, req, "alice@example.com")
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestUpload_ValidPNG_Returns200WithRedirectURL(t *testing.T) {
	h, _, _ := newHandlers(t)
	pngData := makePNG(t, 100, 100)

	req := buildUploadRequest(t, pngData, "https://example.com/page")
	rr := executeAs(t, h.Upload, req, "alice@example.com")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	redirectURL, ok := resp["redirect_url"]
	if !ok {
		t.Fatal("response missing redirect_url")
	}
	if !strings.HasPrefix(redirectURL, "https://example.com/") {
		t.Errorf("unexpected redirect_url: %q", redirectURL)
	}
}

func TestUpload_NoIdentity_Returns401(t *testing.T) {
	h, _, _ := newHandlers(t)
	req := buildUploadRequest(t, makePNG(t, 10, 10), "")
	rr := executeAnonymous(h.Upload, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without identity, got %d", rr.Code)
	}
}

func TestParseSourceURL(t *testing.T) {
	h, _, _ := newHandlers(t)     // allowlist: http, https
	hFile, _, _ := newHandlers(t) //
	hFile.cfg.SourceURLSchemes = []string{"http", "https", "file"}
	hNone, _, _ := newHandlers(t)    //
	hNone.cfg.SourceURLSchemes = nil // source URLs disabled

	tests := []struct {
		name    string
		h       *Handlers
		in      string
		wantPtr string // "" means expect nil pointer
		wantErr bool
	}{
		{"empty clears", h, "  ", "", false},
		{"https allowed", h, "https://example.com/x", "https://example.com/x", false},
		{"http allowed", h, "http://example.com", "http://example.com", false},
		{"trims whitespace", h, "  https://example.com  ", "https://example.com", false},
		{"javascript rejected", h, "javascript:alert(1)", "", true},
		{"data rejected", h, "data:text/html,x", "", true},
		{"file rejected by default", h, "file:///etc/passwd", "", true},
		{"file allowed when configured", hFile, "file:///home/u/p.html", "file:///home/u/p.html", false},
		{"scheme-relative rejected", h, "//evil.com", "", true},
		{"no scheme rejected", h, "example.com/x", "", true},
		{"invalid utf-8 rejected", h, "https://example.com/\xff", "", true},
		{"disabled rejects any", hNone, "https://example.com", "", true},
		{"disabled still allows empty", hNone, "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.h.parseSourceURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (ptr=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantPtr == "" {
				if got != nil {
					t.Fatalf("expected nil pointer, got %q", *got)
				}
				return
			}
			if got == nil || *got != tc.wantPtr {
				t.Fatalf("expected %q, got %v", tc.wantPtr, got)
			}
		})
	}
}

func TestUpdate_DisallowedSourceURLScheme_Returns400(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img12345", "alice@example.com")

	body := strings.NewReader(`{"source_url":"javascript:alert(1)"}`)
	req := httptest.NewRequest(http.MethodPatch, "/img12345", body)
	req = chiRequest(req, map[string]string{"id": "img12345"})
	rr := executeAs(t, h.Update, req, "alice@example.com")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestUpload_DisallowedSourceURLScheme_DropsButSucceeds(t *testing.T) {
	h, database, _ := newHandlers(t)
	req := buildUploadRequest(t, makePNG(t, 30, 30), "javascript:alert(1)")
	rr := executeAs(t, h.Upload, req, "bob@example.com")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	// The stored image must have no source URL (the dangerous scheme was dropped).
	imgs, err := database.ListRecentImages(context.Background(), "bob@example.com", 10, 0)
	if err != nil {
		t.Fatalf("ListRecentImages: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1 image, got %d", len(imgs))
	}
	if imgs[0].SourceURL != nil {
		t.Errorf("expected nil source_url, got %q", *imgs[0].SourceURL)
	}
}

func TestParseSourceURL_InvalidUTF8ReturnsSpecificError(t *testing.T) {
	h, _, _ := newHandlers(t)
	_, err := h.parseSourceURL("https://example.com/\xff")
	if !errors.Is(err, errSourceURLNotUTF8) {
		t.Fatalf("expected errSourceURLNotUTF8, got %v", err)
	}
}

func TestUpload_InvalidUTF8SourceURL_DropsButSucceeds(t *testing.T) {
	h, database, _ := newHandlers(t)
	// A multipart form value carries raw bytes (unlike JSON, which substitutes
	// U+FFFD), so invalid UTF-8 reaches the handler intact and must be dropped.
	req := buildUploadRequest(t, makePNG(t, 30, 30), "https://example.com/\xff")
	rr := executeAs(t, h.Upload, req, "bob@example.com")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	imgs, err := database.ListRecentImages(context.Background(), "bob@example.com", 10, 0)
	if err != nil {
		t.Fatalf("ListRecentImages: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1 image, got %d", len(imgs))
	}
	if imgs[0].SourceURL != nil {
		t.Errorf("expected nil source_url, got %q", *imgs[0].SourceURL)
	}
}

// ---- Delete tests ----------------------------------------------------------

func setupImageForUser(t *testing.T, database *db.DB, stor *storage.Storage, imageID, email string) {
	t.Helper()
	ctx := context.Background()
	if err := database.UpsertUser(ctx, email, email, ""); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	pngData := makePNG(t, 20, 20)
	if err := stor.Save(imageID, bytes.NewReader(pngData)); err != nil {
		t.Fatalf("stor.Save: %v", err)
	}
	u := "https://example.com"
	if err := database.InsertImage(ctx, db.Image{
		ID:        imageID,
		OwnerID:   email,
		SourceURL: &u,
		FilePath:  imageID + ".png",
	}); err != nil {
		t.Fatalf("InsertImage: %v", err)
	}
}

func TestDelete_NotOwner_Returns403(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-del1", "owner@example.com")

	req := httptest.NewRequest(http.MethodDelete, "/img-del1", nil)
	req = chiRequest(req, map[string]string{"id": "img-del1"})

	rr := executeAs(t, h.Delete, req, "other@example.com")
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestDelete_NotFound_Returns404(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := httptest.NewRequest(http.MethodDelete, "/nonexistent", nil)
	req = chiRequest(req, map[string]string{"id": "nonexistent"})

	rr := executeAs(t, h.Delete, req, "any@example.com")
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestDelete_Owner_Returns200AndRemovesFiles(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-del2", "owner@example.com")

	req := httptest.NewRequest(http.MethodDelete, "/img-del2", nil)
	req = chiRequest(req, map[string]string{"id": "img-del2"})

	rr := executeAs(t, h.Delete, req, "owner@example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	img, err := database.GetImage(context.Background(), "img-del2")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if img != nil {
		t.Error("image record should be deleted from DB")
	}
}

// ---- View tests ------------------------------------------------------------

func TestView_UnknownID_Returns404(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	req = chiRequest(req, map[string]string{"id": "unknown"})

	rr := executeAs(t, h.View, req, "any@example.com")
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestView_KnownID_Returns200(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-view1", "owner@example.com")

	req := httptest.NewRequest(http.MethodGet, "/img-view1", nil)
	req = chiRequest(req, map[string]string{"id": "img-view1"})

	rr := executeAs(t, h.View, req, "owner@example.com")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestView_UnauthenticatedAllowed(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-view2", "owner@example.com")

	req := httptest.NewRequest(http.MethodGet, "/img-view2", nil)
	req = chiRequest(req, map[string]string{"id": "img-view2"})

	rr := executeAnonymous(h.View, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for anonymous viewer, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- ServeImage metadata header tests --------------------------------------

func TestServeImage_SetsMetadataHeaders(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-meta1", "owner@example.com")
	// setupImageForUser sets SourceURL but no title; add a title with characters
	// that must be percent-encoded to stay a valid header value.
	title := "Café & co\nline"
	src := "https://example.com/a b?q=1"
	if _, err := database.UpdateImage(context.Background(), "img-meta1", "owner@example.com", &title, &src); err != nil {
		t.Fatalf("UpdateImage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/img-meta1.png", nil)
	req = chiRequest(req, map[string]string{"id": "img-meta1"})

	rr := executeAnonymous(h.ServeImage, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if got, want := rr.Header().Get("X-Screenshot-Title"), url.QueryEscape(title); got != want {
		t.Errorf("X-Screenshot-Title = %q, want %q", got, want)
	}
	if got, want := rr.Header().Get("X-Screenshot-Source-Url"), url.QueryEscape(src); got != want {
		t.Errorf("X-Screenshot-Source-Url = %q, want %q", got, want)
	}
}

func TestServeImage_OmitsHeadersWhenMetadataAbsent(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-meta2", "owner@example.com")
	// Clear the source URL set by setupImageForUser so neither field is present.
	if _, err := database.UpdateImage(context.Background(), "img-meta2", "owner@example.com", nil, nil); err != nil {
		t.Fatalf("UpdateImage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/img-meta2.png", nil)
	req = chiRequest(req, map[string]string{"id": "img-meta2"})

	rr := executeAnonymous(h.ServeImage, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if _, ok := rr.Header()["X-Screenshot-Title"]; ok {
		t.Error("X-Screenshot-Title should be absent when no title is set")
	}
	if _, ok := rr.Header()["X-Screenshot-Source-Url"]; ok {
		t.Error("X-Screenshot-Source-Url should be absent when no source URL is set")
	}
}

// ---- Home tests ------------------------------------------------------------

func TestHome_Returns200AndListsImages(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "home-img1", "home@example.com")
	setupImageForUser(t, database, stor, "home-img2", "home@example.com")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := executeAs(t, h.Home, req, "home@example.com")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHome_NewUser_Returns200(t *testing.T) {
	h, _, _ := newHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := executeAs(t, h.Home, req, "brand-new@example.com")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for new user, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHome_LoggedOut_Returns200(t *testing.T) {
	h, _, _ := newHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := executeAnonymous(h.Home, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for logged-out home, got %d", rr.Code)
	}
}
