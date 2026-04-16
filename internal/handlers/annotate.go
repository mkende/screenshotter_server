package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/db"
	"github.com/mkende/screenshotter/server/internal/storage"
)

type annotateData struct {
	User  *auth.Claims
	Image *db.Image
}

// AnnotateView serves the annotation editor page for GET /{id}/annotate.
func (h *Handlers) AnnotateView(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")

	img, err := h.db.GetImage(r.Context(), id)
	if err != nil {
		slog.Error("get image for annotate view", "id", id, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if img == nil {
		http.NotFound(w, r)
		return
	}
	if img.OwnerID != claims.UserID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.renderTemplate(w, "annotate.html", annotateData{User: claims, Image: img})
}

// Annotate handles POST /{id}/annotate: replaces the stored PNG with the
// annotated version rendered client-side by Fabric.js.
func (h *Handlers) Annotate(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")

	img, err := h.db.GetImage(r.Context(), id)
	if err != nil {
		slog.Error("get image for annotate save", "id", id, "err", err)
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

	maxBytes := h.cfg.Server.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if _, err := h.storage.Save(id, r.Body); err != nil {
		if errors.Is(err, storage.ErrNotPNG) {
			writeJSONError(w, http.StatusBadRequest, "uploaded file is not a valid PNG image")
			return
		}
		slog.Error("save annotated image", "id", id, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to save annotated image")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{})
}
