package handlers

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter_server/internal/auth"
)

// ServeImage serves the raw PNG for GET /{id}.png. Accessible to
// unauthenticated users; rate-limited at the router level to prevent
// enumeration.
func (h *Handlers) ServeImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.serveFile(w, r, id, h.storage.ImagePath(id))
}

// ServeThumb serves the thumbnail PNG for GET /thumb/{id}.png.
// Authentication is required and only the owner may fetch the thumbnail.
func (h *Handlers) ServeThumb(w http.ResponseWriter, r *http.Request) {
	identity := auth.FromContext(r.Context())
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
	if identity == nil || (img.OwnerID != identity.Email && !identity.IsAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	h.serveFilePath(w, r, id, h.storage.ThumbPath(id))
}

func (h *Handlers) serveFile(w http.ResponseWriter, r *http.Request, id, path string) {
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
	// Expose the screenshot metadata as headers so clients fetching the raw PNG
	// can read the title and origin without a second request. Values are
	// percent-encoded so arbitrary user text stays a valid (ASCII) header value;
	// since both fields are validated as UTF-8 at write time (see Update and
	// parseSourceURL), the percent-decoded value is always valid UTF-8.
	if img.Title != nil {
		w.Header().Set("X-Screenshot-Title", url.QueryEscape(*img.Title))
	}
	if img.SourceURL != nil {
		w.Header().Set("X-Screenshot-Source-Url", url.QueryEscape(*img.SourceURL))
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
