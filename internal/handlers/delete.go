package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/httputil"
)

// Delete handles DELETE /{id}: removes the image if the caller is its owner.
func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	identity := auth.FromContext(r.Context())
	if identity == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")

	img, err := h.db.GetImage(r.Context(), id)
	if err != nil {
		slog.Error("get image for delete", "id", id, "err", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if img == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "image not found")
		return
	}
	if img.OwnerID != identity.Email {
		httputil.WriteJSONError(w, http.StatusForbidden, "not the owner of this image")
		return
	}

	// Delete from DB first; if the file removal fails the record is still gone,
	// which is acceptable — the orphaned file can be cleaned up separately.
	deleted, err := h.db.DeleteImage(r.Context(), id, identity.Email)
	if err != nil {
		slog.Error("delete image record", "id", id, "err", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		httputil.WriteJSONError(w, http.StatusNotFound, "image not found")
		return
	}
	if err := h.storage.Delete(id); err != nil {
		slog.Error("delete image file", "id", id, "err", err)
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{})
}
