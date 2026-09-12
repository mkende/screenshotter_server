package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/mkende/screenshotter_server/internal/auth"
	"github.com/mkende/screenshotter_server/internal/db"
	"github.com/mkende/screenshotter_server/internal/storage"
)

// adminIdentityFor returns an admin Identity for email.
func adminIdentityFor(email string) *auth.Identity {
	return &auth.Identity{
		Email:       email,
		DisplayName: email,
		IsAdmin:     true,
		Source:      auth.AuthSourceTailscale,
	}
}

// executeAsAdmin calls h with an admin identity injected into the context.
func executeAsAdmin(t *testing.T, h http.HandlerFunc, req *http.Request, adminEmail string) *httptest.ResponseRecorder {
	t.Helper()
	req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentityFor(adminEmail)))
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

// withRequireAdmin wraps h in the RequireAdmin middleware, using a simple 403
// denied handler for non-admin HTML requests.
func withRequireAdmin(h http.HandlerFunc) http.Handler {
	denied := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	return auth.RequireAdmin(denied)(h)
}

// jsonBody encodes v to JSON and returns it as a *bytes.Reader.
func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return bytes.NewReader(b)
}

// ---- Access control tests --------------------------------------------------

// TestAdminRoutes_NonAdminCantAccess verifies that every admin mutation endpoint
// returns 403 when the caller is unauthenticated or lacks IsAdmin.
func TestAdminRoutes_NonAdminCantAccess(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "ac-img1", "ac-owner@example.com")
	if err := database.UpsertUser(context.Background(), "ac-target@example.com", "Target", ""); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	reassignBody := jsonBody(t, map[string]string{"to": "ac-target@example.com"})

	cases := []struct {
		name    string
		method  string
		path    string
		params  map[string]string
		body    *bytes.Reader
		handler http.HandlerFunc
	}{
		{
			name: "AdminUsers GET", method: http.MethodGet, path: "/admin",
			handler: h.AdminUsers,
		},
		{
			name: "AdminUserDetail GET", method: http.MethodGet, path: "/admin/users/ac-owner@example.com",
			params:  map[string]string{"email": "ac-owner@example.com"},
			handler: h.AdminUserDetail,
		},
		{
			name: "AdminDeleteUser DELETE", method: http.MethodDelete, path: "/admin/users/ac-owner@example.com",
			params:  map[string]string{"email": "ac-owner@example.com"},
			handler: h.AdminDeleteUser,
		},
		{
			name: "AdminDeleteImage DELETE", method: http.MethodDelete, path: "/admin/images/ac-img1",
			params:  map[string]string{"id": "ac-img1"},
			handler: h.AdminDeleteImage,
		},
		{
			name: "AdminReassignImage POST", method: http.MethodPost, path: "/admin/images/ac-img1/reassign",
			params:  map[string]string{"id": "ac-img1"},
			body:    reassignBody,
			handler: h.AdminReassignImage,
		},
		{
			name: "AdminReassignAll POST", method: http.MethodPost, path: "/admin/users/ac-owner@example.com/reassign-all",
			params:  map[string]string{"email": "ac-owner@example.com"},
			body:    reassignBody,
			handler: h.AdminReassignAll,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, scenario := range []struct {
				label string
				ctx   func(*http.Request) *http.Request
			}{
				{"unauthenticated", func(r *http.Request) *http.Request { return r }},
				{"non-admin", func(r *http.Request) *http.Request {
					return r.WithContext(auth.WithIdentity(r.Context(), identityFor("regular@example.com")))
				}},
			} {
				t.Run(scenario.label, func(t *testing.T) {
					var body io.Reader
					if tc.body != nil {
						tc.body.Seek(0, 0) //nolint:errcheck
						body = tc.body
					}
					req := httptest.NewRequest(tc.method, tc.path, body)
					if tc.params != nil {
						req = chiRequest(req, tc.params)
					}
					req = scenario.ctx(req)

					rr := httptest.NewRecorder()
					withRequireAdmin(tc.handler).ServeHTTP(rr, req)

					if rr.Code != http.StatusForbidden {
						t.Errorf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
					}
				})
			}
		})
	}
}

// ---- AdminUsers tests ------------------------------------------------------

func TestAdminUsers_Returns200(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "au-img1", "au-user@example.com")

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := executeAsAdmin(t, h.AdminUsers, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminUsers_SearchFilterApplied(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "au-s-img1", "au-search-me@example.com")
	if err := database.UpsertUser(context.Background(), "au-other-user@example.com", "Other", ""); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin?q=au-search-me", nil)
	rr := executeAsAdmin(t, h.AdminUsers, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- AdminUserDetail tests -------------------------------------------------

func TestAdminUserDetail_UnknownUser_Returns404(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/users/nobody@example.com", nil)
	req = chiRequest(req, map[string]string{"email": "nobody@example.com"})

	rr := executeAsAdmin(t, h.AdminUserDetail, req, "admin@example.com")
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminUserDetail_KnownUser_Returns200(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "aud-img1", "aud-user@example.com")

	req := httptest.NewRequest(http.MethodGet, "/admin/users/aud-user@example.com", nil)
	req = chiRequest(req, map[string]string{"email": "aud-user@example.com"})

	rr := executeAsAdmin(t, h.AdminUserDetail, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- AdminDeleteUser tests -------------------------------------------------

func TestAdminDeleteUser_DeletesUserImageAndFiles(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "adu-img1", "adu-owner@example.com")
	setupImageForUser(t, database, stor, "adu-img2", "adu-owner@example.com")

	req := httptest.NewRequest(http.MethodDelete, "/admin/users/adu-owner@example.com", nil)
	req = chiRequest(req, map[string]string{"email": "adu-owner@example.com"})

	rr := executeAsAdmin(t, h.AdminDeleteUser, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// User gone from DB.
	u, err := database.GetUser(context.Background(), "adu-owner@example.com")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u != nil {
		t.Error("user should be deleted from DB")
	}

	// Images cascade-deleted from DB.
	for _, id := range []string{"adu-img1", "adu-img2"} {
		img, err := database.GetImage(context.Background(), id)
		if err != nil {
			t.Fatalf("GetImage %s: %v", id, err)
		}
		if img != nil {
			t.Errorf("image %s should be cascade-deleted", id)
		}
	}

	// Image files removed from storage.
	for _, id := range []string{"adu-img1", "adu-img2"} {
		if _, err := os.Stat(stor.ImagePath(id)); !os.IsNotExist(err) {
			t.Errorf("image file %s should be deleted from disk", id)
		}
		if _, err := os.Stat(stor.ThumbPath(id)); !os.IsNotExist(err) {
			t.Errorf("thumb file %s should be deleted from disk", id)
		}
	}
}

// TestAdminDeleteUser_SomeFilesMissing_StillSucceeds verifies that if some
// image files are already absent from disk (e.g. due to prior corruption), the
// delete still returns 200 and removes all DB records.
func TestAdminDeleteUser_SomeFilesMissing_StillSucceeds(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "adufm-img1", "adufm@example.com")
	setupImageForUser(t, database, stor, "adufm-img2", "adufm@example.com")
	setupImageForUser(t, database, stor, "adufm-img3", "adufm@example.com")

	// Manually remove one image and its thumbnail from disk to simulate missing files.
	os.Remove(stor.ImagePath("adufm-img2")) //nolint:errcheck
	os.Remove(stor.ThumbPath("adufm-img2")) //nolint:errcheck

	req := httptest.NewRequest(http.MethodDelete, "/admin/users/adufm@example.com", nil)
	req = chiRequest(req, map[string]string{"email": "adufm@example.com"})

	rr := executeAsAdmin(t, h.AdminDeleteUser, req, "admin@example.com")
	// Missing files must NOT cause a 500 — storage.Delete ignores not-found errors.
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 even with missing files, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// All DB records must be gone regardless.
	u, err := database.GetUser(context.Background(), "adufm@example.com")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u != nil {
		t.Error("user should be deleted from DB despite missing files")
	}
	for _, id := range []string{"adufm-img1", "adufm-img2", "adufm-img3"} {
		img, err := database.GetImage(context.Background(), id)
		if err != nil {
			t.Fatalf("GetImage %s: %v", id, err)
		}
		if img != nil {
			t.Errorf("image %s should be cascade-deleted even though its file was missing", id)
		}
	}
}

func TestAdminDeleteUser_UnknownUser_Returns404(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := httptest.NewRequest(http.MethodDelete, "/admin/users/nobody@example.com", nil)
	req = chiRequest(req, map[string]string{"email": "nobody@example.com"})

	rr := executeAsAdmin(t, h.AdminDeleteUser, req, "admin@example.com")
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- AdminDeleteImage tests ------------------------------------------------

func TestAdminDeleteImage_DeletesImageAndFiles(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "adi-img1", "adi-owner@example.com")

	req := httptest.NewRequest(http.MethodDelete, "/admin/images/adi-img1", nil)
	req = chiRequest(req, map[string]string{"id": "adi-img1"})

	rr := executeAsAdmin(t, h.AdminDeleteImage, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	img, err := database.GetImage(context.Background(), "adi-img1")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if img != nil {
		t.Error("image should be deleted from DB")
	}

	if _, err := os.Stat(stor.ImagePath("adi-img1")); !os.IsNotExist(err) {
		t.Error("image file should be deleted from disk")
	}
}

// TestAdminDeleteImage_FileMissingOnDisk_StillDeletesDBRecord verifies that a
// missing storage file does not prevent the DB record from being cleaned up.
func TestAdminDeleteImage_FileMissingOnDisk_StillDeletesDBRecord(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "adifm-img1", "adifm-owner@example.com")

	// Remove the file from disk before deleting via the admin handler.
	os.Remove(stor.ImagePath("adifm-img1")) //nolint:errcheck
	os.Remove(stor.ThumbPath("adifm-img1")) //nolint:errcheck

	req := httptest.NewRequest(http.MethodDelete, "/admin/images/adifm-img1", nil)
	req = chiRequest(req, map[string]string{"id": "adifm-img1"})

	rr := executeAsAdmin(t, h.AdminDeleteImage, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 even with missing file, got %d; body: %s", rr.Code, rr.Body.String())
	}

	img, err := database.GetImage(context.Background(), "adifm-img1")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if img != nil {
		t.Error("DB record should be deleted even though file was already missing")
	}
}

func TestAdminDeleteImage_NotFound_Returns404(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := httptest.NewRequest(http.MethodDelete, "/admin/images/nonexistent", nil)
	req = chiRequest(req, map[string]string{"id": "nonexistent"})

	rr := executeAsAdmin(t, h.AdminDeleteImage, req, "admin@example.com")
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- AdminReassignImage tests -----------------------------------------------

func setupTwoUsers(t *testing.T, database *db.DB, stor *storage.Storage, imageID, fromEmail, toEmail string) {
	t.Helper()
	setupImageForUser(t, database, stor, imageID, fromEmail)
	if err := database.UpsertUser(context.Background(), toEmail, toEmail, ""); err != nil {
		t.Fatalf("UpsertUser %q: %v", toEmail, err)
	}
}

func TestAdminReassignImage_ChangesOwner(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupTwoUsers(t, database, stor, "ari-img1", "ari-from@example.com", "ari-to@example.com")

	req := httptest.NewRequest(http.MethodPost, "/admin/images/ari-img1/reassign",
		jsonBody(t, map[string]string{"to": "ari-to@example.com"}))
	req.Header.Set("Content-Type", "application/json")
	req = chiRequest(req, map[string]string{"id": "ari-img1"})

	rr := executeAsAdmin(t, h.AdminReassignImage, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	img, err := database.GetImage(context.Background(), "ari-img1")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if img.OwnerID != "ari-to@example.com" {
		t.Errorf("OwnerID after reassign: got %q, want ari-to@example.com", img.OwnerID)
	}
}

func TestAdminReassignImage_UnknownTargetUser_Returns422(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "ari-img2", "ari2-from@example.com")

	req := httptest.NewRequest(http.MethodPost, "/admin/images/ari-img2/reassign",
		jsonBody(t, map[string]string{"to": "ari2-nonexistent@example.com"}))
	req.Header.Set("Content-Type", "application/json")
	req = chiRequest(req, map[string]string{"id": "ari-img2"})

	rr := executeAsAdmin(t, h.AdminReassignImage, req, "admin@example.com")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for unknown target user, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminReassignImage_UnknownImage_Returns404(t *testing.T) {
	h, database, _ := newHandlers(t)
	if err := database.UpsertUser(context.Background(), "ari3-to@example.com", "To", ""); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/images/nonexistent/reassign",
		jsonBody(t, map[string]string{"to": "ari3-to@example.com"}))
	req.Header.Set("Content-Type", "application/json")
	req = chiRequest(req, map[string]string{"id": "nonexistent"})

	rr := executeAsAdmin(t, h.AdminReassignImage, req, "admin@example.com")
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown image, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminReassignImage_MissingBody_Returns400(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/admin/images/any/reassign",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req = chiRequest(req, map[string]string{"id": "any"})

	rr := executeAsAdmin(t, h.AdminReassignImage, req, "admin@example.com")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing 'to' field, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- AdminReassignAll tests -------------------------------------------------

func TestAdminReassignAll_MovesAllImages(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupTwoUsers(t, database, stor, "ara-img1", "ara-from@example.com", "ara-to@example.com")
	setupImageForUser(t, database, stor, "ara-img2", "ara-from@example.com")

	req := httptest.NewRequest(http.MethodPost, "/admin/users/ara-from@example.com/reassign-all",
		jsonBody(t, map[string]string{"to": "ara-to@example.com"}))
	req.Header.Set("Content-Type", "application/json")
	req = chiRequest(req, map[string]string{"email": "ara-from@example.com"})

	rr := executeAsAdmin(t, h.AdminReassignAll, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]int64
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["moved"] != 2 {
		t.Errorf("moved count: got %d, want 2", resp["moved"])
	}

	// Verify ownership changed.
	for _, id := range []string{"ara-img1", "ara-img2"} {
		img, err := database.GetImage(context.Background(), id)
		if err != nil {
			t.Fatalf("GetImage %s: %v", id, err)
		}
		if img.OwnerID != "ara-to@example.com" {
			t.Errorf("image %s OwnerID: got %q, want ara-to@example.com", id, img.OwnerID)
		}
	}
}

func TestAdminReassignAll_UnknownTargetUser_Returns422(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "ara2-img1", "ara2-from@example.com")

	req := httptest.NewRequest(http.MethodPost, "/admin/users/ara2-from@example.com/reassign-all",
		jsonBody(t, map[string]string{"to": "ara2-nobody@example.com"}))
	req.Header.Set("Content-Type", "application/json")
	req = chiRequest(req, map[string]string{"email": "ara2-from@example.com"})

	rr := executeAsAdmin(t, h.AdminReassignAll, req, "admin@example.com")
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for unknown target user, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminReassignAll_MissingBody_Returns400(t *testing.T) {
	h, _, _ := newHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/admin/users/someone@example.com/reassign-all",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req = chiRequest(req, map[string]string{"email": "someone@example.com"})

	rr := executeAsAdmin(t, h.AdminReassignAll, req, "admin@example.com")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing 'to' field, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ---- ServeThumb access tests -----------------------------------------------

func TestServeThumb_AdminAccessesOtherUsersThumbnail(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "stt-img1", "stt-owner@example.com")

	req := httptest.NewRequest(http.MethodGet, "/thumb/stt-img1.png", nil)
	req = chiRequest(req, map[string]string{"id": "stt-img1"})

	// Admin whose email is different from the image owner.
	rr := executeAsAdmin(t, h.ServeThumb, req, "admin@example.com")
	if rr.Code != http.StatusOK {
		t.Errorf("admin should be able to view any thumbnail, got %d", rr.Code)
	}
}

func TestServeThumb_NonOwnerNonAdmin_Returns403(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "stt-img2", "stt-owner2@example.com")

	req := httptest.NewRequest(http.MethodGet, "/thumb/stt-img2.png", nil)
	req = chiRequest(req, map[string]string{"id": "stt-img2"})

	rr := executeAs(t, h.ServeThumb, req, "other@example.com")
	if rr.Code != http.StatusForbidden {
		t.Errorf("non-owner non-admin should get 403, got %d", rr.Code)
	}
}

func TestServeThumb_Owner_Returns200(t *testing.T) {
	h, database, stor := newHandlers(t)
	setupImageForUser(t, database, stor, "stt-img3", "stt-owner3@example.com")

	req := httptest.NewRequest(http.MethodGet, "/thumb/stt-img3.png", nil)
	req = chiRequest(req, map[string]string{"id": "stt-img3"})

	rr := executeAs(t, h.ServeThumb, req, "stt-owner3@example.com")
	if rr.Code != http.StatusOK {
		t.Errorf("owner should get 200, got %d", rr.Code)
	}
}
