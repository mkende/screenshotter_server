package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter_server/internal/auth"
	"github.com/mkende/screenshotter_server/internal/db"
	"github.com/mkende/screenshotter_server/internal/httputil"
	"github.com/mkende/screenshotter_server/internal/storage"
)

type annotateData struct {
	pageData
	Image *db.Image
}

// AnnotateView serves the annotation editor page for GET /{id}/annotate.
func (h *Handlers) AnnotateView(w http.ResponseWriter, r *http.Request) {
	identity := auth.FromContext(r.Context())
	if identity == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
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
	if img.OwnerID != identity.Email {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.renderTemplate(w, "annotate.html", annotateData{
		pageData: h.newPageData(r),
		Image:    img,
	})
}

// Annotate handles POST /{id}/annotate: replaces the stored PNG with the
// annotated version rendered client-side by Fabric.js.
func (h *Handlers) Annotate(w http.ResponseWriter, r *http.Request) {
	identity := auth.FromContext(r.Context())
	if identity == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")

	img, err := h.db.GetImage(r.Context(), id)
	if err != nil {
		slog.Error("get image for annotate save", "id", id, "err", err)
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

	maxBytes := h.cfg.Server.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := h.storage.Save(id, r.Body); err != nil {
		if errors.Is(err, storage.ErrNotPNG) {
			httputil.WriteJSONError(w, http.StatusBadRequest, "uploaded file is not a valid PNG image")
			return
		}
		if errors.Is(err, storage.ErrImageTooLarge) {
			httputil.WriteJSONError(w, http.StatusBadRequest, "image dimensions are too large")
			return
		}
		slog.Error("save annotated image", "id", id, "err", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "failed to save annotated image")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{})
}
