package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
	"github.com/mkende/screenshotter/server/internal/db"
	"github.com/mkende/screenshotter/server/internal/storage"
)

// ---- helpers ---------------------------------------------------------------

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

// minimalTemplates returns a map with simple stub templates that satisfy
// the handlers' renderTemplate calls without needing embedded FS.
func minimalTemplates() map[string]*template.Template {
	const stub = `{{define "base"}}OK{{end}}`
	pages := []string{"home.html", "view.html"}
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

	cfg := &config.Config{}
	cfg.Server.Domain = "https://example.com"
	cfg.Server.MaxUploadMB = 4
	cfg.ID.Length = 8

	h := New(cfg, database, stor, nil /* auth not needed for handler tests */, minimalTemplates(), nil /* font not needed for handler tests */)
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

// claimsForUser returns a minimal Claims for a given userID.
func claimsForUser(userID string) *auth.Claims {
	return &auth.Claims{
		UserID:      userID,
		DisplayName: userID,
		Email:       userID + "@example.com",
	}
}

// authService builds a real auth.Service using a Tailscale backend with no
// CIDR restriction so it trusts any remote address that provides the right
// headers.
func authService(t *testing.T) *auth.Service {
	t.Helper()
	cfg := &config.Config{}
	cfg.Server.Domain = "https://example.com"
	cfg.Session.Secret = "a-secret-that-is-at-least-32-characters-long"
	cfg.Auth.Backend = "tailscale"
	// No proxy_ips → trust everyone.
	svc, err := auth.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return svc
}

// executeWithUser runs handler h with claims for userID already in context,
// by routing through a Tailscale auth middleware injecting the right headers.
func executeWithUser(t *testing.T, h http.HandlerFunc, req *http.Request, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req.Header.Set("Tailscale-User-Login", userID)
	req.Header.Set("Tailscale-User-Name", userID)
	// Tailscale backend reads RemoteAddr; no CIDR restriction so any addr works.
	req.RemoteAddr = "127.0.0.1:1234"

	svc := authService(t)
	rr := httptest.NewRecorder()
	svc.Middleware(h).ServeHTTP(rr, req)
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

	// Build a request with only source_url, no image field.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("source_url", "https://example.com") //nolint:errcheck
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rr := executeWithUser(t, h.Upload, req, "alice")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestUpload_NonPNGBytes_Returns400(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := buildUploadRequest(t, []byte("this is not a png"), "https://example.com")
	rr := executeWithUser(t, h.Upload, req, "alice")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-PNG, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not a valid PNG") {
		t.Errorf("expected PNG error message, got: %s", rr.Body.String())
	}
}

func TestUpload_OversizedBody_Returns413(t *testing.T) {
	h, _, _ := newHandlers(t)
	// MaxUploadMB=4, so 5MB body should be rejected.
	bigData := make([]byte, 5<<20)
	// Put a fake PNG magic at the start to get past the earliest check,
	// but the body is so large it hits the MaxBytesReader limit.
	copy(bigData, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("image", "big.png")
	fw.Write(bigData) //nolint:errcheck
	mw.WriteField("source_url", "https://example.com") //nolint:errcheck
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rr := executeWithUser(t, h.Upload, req, "alice")
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestUpload_ValidPNG_Returns200WithRedirectURL(t *testing.T) {
	h, _, _ := newHandlers(t)
	pngData := makePNG(t, 100, 100)

	req := buildUploadRequest(t, pngData, "https://example.com/page")
	rr := executeWithUser(t, h.Upload, req, "alice")

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

func TestUpload_MissingSourceURL_Returns200(t *testing.T) {
	h, _, _ := newHandlers(t)
	pngData := makePNG(t, 10, 10)

	// source_url is optional; omitting it should still succeed.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("image", "shot.png")
	fw.Write(pngData) //nolint:errcheck
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rr := executeWithUser(t, h.Upload, req, "alice")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for missing source_url, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- Delete tests ----------------------------------------------------------

func setupImageForUser(t *testing.T, database *db.DB, stor *storage.Storage, imageID, userID string) {
	t.Helper()
	ctx := context.Background()
	if err := database.UpsertUser(ctx, userID, userID, userID+"@example.com"); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	pngData := makePNG(t, 20, 20)
	filePath, err := stor.Save(imageID, bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("stor.Save: %v", err)
	}
	u := "https://example.com"
	if err := database.InsertImage(ctx, db.Image{
		ID:        imageID,
		OwnerID:   userID,
		SourceURL: &u,
		FilePath:  filePath,
	}); err != nil {
		t.Fatalf("InsertImage: %v", err)
	}
}

func TestDelete_NotOwner_Returns403(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-del1", "owner-user")

	req := httptest.NewRequest(http.MethodDelete, "/img-del1", nil)
	req = chiRequest(req, map[string]string{"id": "img-del1"})

	rr := executeWithUser(t, h.Delete, req, "other-user")
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestDelete_NotFound_Returns404(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := httptest.NewRequest(http.MethodDelete, "/nonexistent", nil)
	req = chiRequest(req, map[string]string{"id": "nonexistent"})

	rr := executeWithUser(t, h.Delete, req, "any-user")
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestDelete_Owner_Returns200AndRemovesFiles(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-del2", "the-owner")

	req := httptest.NewRequest(http.MethodDelete, "/img-del2", nil)
	req = chiRequest(req, map[string]string{"id": "img-del2"})

	rr := executeWithUser(t, h.Delete, req, "the-owner")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Confirm image record is gone from DB.
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

	rr := executeWithUser(t, h.View, req, "any-user")
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestView_KnownID_Returns200(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "img-view1", "view-owner")

	req := httptest.NewRequest(http.MethodGet, "/img-view1", nil)
	req = chiRequest(req, map[string]string{"id": "img-view1"})

	rr := executeWithUser(t, h.View, req, "view-owner")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- Home tests ------------------------------------------------------------

func TestHome_Returns200AndListsImages(t *testing.T) {
	h, database, stor := newHandlers(t)
	// Pre-create some images for the user.
	setupImageForUser(t, database, stor, "home-img1", "home-user")
	setupImageForUser(t, database, stor, "home-img2", "home-user")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := executeWithUser(t, h.Home, req, "home-user")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHome_NewUser_Returns200(t *testing.T) {
	h, _, _ := newHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Brand-new user: Home should upsert them and return 200 with no images.
	rr := executeWithUser(t, h.Home, req, "brand-new-user")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for new user, got %d; body: %s", rr.Code, rr.Body.String())
	}
}
