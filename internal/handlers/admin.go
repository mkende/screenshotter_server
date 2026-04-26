package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter/server/internal/db"
)

const adminPageSize = 50

type adminUsersData struct {
	pageData
	Users   []db.UserWithStats
	Query   string
	Page    int
	PrevPage int
	NextPage int
	HasPrev bool
	HasNext bool
	Total   int
}

type adminUserDetailData struct {
	pageData
	TargetUser db.UserWithStats
	Images     []db.Image
	Page       int
	PrevPage   int
	NextPage   int
	HasPrev    bool
	HasNext    bool
}

// AdminUsers handles GET /admin: paginated, searchable list of all users.
func (h *Handlers) AdminUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		page = p
	}
	offset := (page - 1) * adminPageSize

	users, total, err := h.db.ListUsers(r.Context(), query, adminPageSize+1, offset)
	if err != nil {
		slog.Error("admin list users", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hasNext := len(users) > adminPageSize
	if hasNext {
		users = users[:adminPageSize]
	}

	h.renderTemplate(w, "admin-users.html", adminUsersData{
		pageData: h.newPageData(r),
		Users:    users,
		Query:    query,
		Page:     page,
		PrevPage: page - 1,
		NextPage: page + 1,
		HasPrev:  page > 1,
		HasNext:  hasNext,
		Total:    total,
	})
}

// AdminUserDetail handles GET /admin/users/{email}: screenshots for a single user.
func (h *Handlers) AdminUserDetail(w http.ResponseWriter, r *http.Request) {
	email := adminEmailParam(r)

	user, err := h.db.GetUserWithStats(r.Context(), email)
	if err != nil {
		slog.Error("admin get user", "email", email, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.NotFound(w, r)
		return
	}

	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		page = p
	}
	offset := (page - 1) * adminPageSize

	imgs, err := h.db.ListRecentImages(r.Context(), email, adminPageSize+1, offset)
	if err != nil {
		slog.Error("admin list images for user", "email", email, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hasNext := len(imgs) > adminPageSize
	if hasNext {
		imgs = imgs[:adminPageSize]
	}

	h.renderTemplate(w, "admin-user-detail.html", adminUserDetailData{
		pageData:   h.newPageData(r),
		TargetUser: *user,
		Images:     imgs,
		Page:       page,
		PrevPage:   page - 1,
		NextPage:   page + 1,
		HasPrev:    page > 1,
		HasNext:    hasNext,
	})
}

// AdminDeleteUser handles DELETE /admin/users/{email}: deletes a user and all
// their images (DB cascade), then cleans up image files from storage.
func (h *Handlers) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	email := adminEmailParam(r)

	imageIDs, err := h.db.ListImageIDsByOwner(r.Context(), email)
	if err != nil {
		slog.Error("admin list image ids for delete user", "email", email, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	deleted, err := h.db.DeleteUser(r.Context(), email)
	if err != nil {
		slog.Error("admin delete user", "email", email, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}

	for _, id := range imageIDs {
		if err := h.storage.Delete(id); err != nil {
			slog.Error("admin delete image file on user delete", "id", id, "err", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{})
}

// AdminDeleteImage handles DELETE /admin/images/{id}: deletes any image
// regardless of ownership.
func (h *Handlers) AdminDeleteImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	deleted, err := h.db.AdminDeleteImage(r.Context(), id)
	if err != nil {
		slog.Error("admin delete image", "id", id, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		writeJSONError(w, http.StatusNotFound, "image not found")
		return
	}

	if err := h.storage.Delete(id); err != nil {
		slog.Error("admin delete image file", "id", id, "err", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{})
}

// AdminReassignImage handles POST /admin/images/{id}/reassign: moves a single
// image to a new owner. Body: {"to": "newowner@example.com"}.
func (h *Handlers) AdminReassignImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.To == "" {
		writeJSONError(w, http.StatusBadRequest, "missing or invalid 'to' field")
		return
	}

	target, err := h.db.GetUser(r.Context(), body.To)
	if err != nil {
		slog.Error("admin reassign image: get target user", "to", body.To, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if target == nil {
		writeJSONError(w, http.StatusUnprocessableEntity, "target user not found")
		return
	}

	ok, err := h.db.ReassignImage(r.Context(), id, body.To)
	if err != nil {
		slog.Error("admin reassign image", "id", id, "to", body.To, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "image not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{})
}

// AdminReassignAll handles POST /admin/users/{email}/reassign-all: moves all
// images from one user to another. Body: {"to": "newowner@example.com"}.
func (h *Handlers) AdminReassignAll(w http.ResponseWriter, r *http.Request) {
	email := adminEmailParam(r)

	var body struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.To == "" {
		writeJSONError(w, http.StatusBadRequest, "missing or invalid 'to' field")
		return
	}

	target, err := h.db.GetUser(r.Context(), body.To)
	if err != nil {
		slog.Error("admin reassign all: get target user", "to", body.To, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if target == nil {
		writeJSONError(w, http.StatusUnprocessableEntity, "target user not found")
		return
	}

	n, err := h.db.ReassignAllImages(r.Context(), email, body.To)
	if err != nil {
		slog.Error("admin reassign all images", "from", email, "to", body.To, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"moved": n})
}

// adminEmailParam extracts and URL-path-unescapes the {email} chi URL param.
func adminEmailParam(r *http.Request) string {
	raw := chi.URLParam(r, "email")
	if unescaped, err := url.PathUnescape(raw); err == nil {
		return unescaped
	}
	return raw
}
