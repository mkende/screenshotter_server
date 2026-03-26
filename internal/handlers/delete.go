package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter/server/internal/auth"
)

// Delete handles DELETE /{id}: removes the image if the caller is its owner.
func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")

	// Verify the image exists and is owned by the caller.
	img, err := h.db.GetImage(r.Context(), id)
	if err != nil {
		slog.Error("get image for delete", "id", id, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if img == nil {
		writeJSONError(w, http.StatusNotFound, "image not found")
		return
	}
	if img.OwnerID != claims.UserID {
		writeJSONError(w, http.StatusForbidden, "not the owner of this image")
		return
	}

	// Delete from DB first; if the file removal fails the record is still gone,
	// which is acceptable — the orphaned file can be cleaned up separately.
	deleted, err := h.db.DeleteImage(r.Context(), id, claims.UserID)
	if err != nil {
		slog.Error("delete image record", "id", id, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		writeJSONError(w, http.StatusNotFound, "image not found")
		return
	}
	if err := h.storage.Delete(id); err != nil {
		// Log but do not fail — the DB record is already gone.
		slog.Error("delete image file", "id", id, "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}
