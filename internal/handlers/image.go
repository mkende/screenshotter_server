package handlers

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter/server/internal/auth"
)

// ServeImage serves the raw PNG for GET /{id}.png.
func (h *Handlers) ServeImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.serveFile(w, r, id, h.storage.ImagePath(id))
}

// ServeThumb serves the thumbnail PNG for GET /thumb/{id}.png.
// Only the owner of the image may fetch its thumbnail.
func (h *Handlers) ServeThumb(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")

	img, err := h.db.GetImage(r.Context(), id)
	if err != nil {
		slog.Error("get image for thumb", "id", id, "err", err)
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

	h.serveFilePath(w, r, id, h.storage.ThumbPath(id))
}

func (h *Handlers) serveFile(w http.ResponseWriter, r *http.Request, id, path string) {
	// Verify the image record exists in the DB before serving the file.
	img, err := h.db.GetImage(r.Context(), id)
	if err != nil {
		slog.Error("get image record for file serve", "id", id, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if img == nil {
		http.NotFound(w, r)
		return
	}
	h.serveFilePath(w, r, id, path)
}

func (h *Handlers) serveFilePath(w http.ResponseWriter, r *http.Request, id, path string) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		slog.Error("open image file", "path", path, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		slog.Error("stat image file", "path", path, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, id+".png", stat.ModTime(), f)
}
