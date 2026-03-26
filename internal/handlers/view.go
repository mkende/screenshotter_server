package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/db"
)

type viewData struct {
	User      *auth.Claims
	Image     *db.Image
	IsOwner   bool
	CurrentURL string
}

// View renders the HTML view page for a single image.
func (h *Handlers) View(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")

	img, err := h.db.GetImage(r.Context(), id)
	if err != nil {
		slog.Error("get image for view", "id", id, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if img == nil {
		http.NotFound(w, r)
		return
	}
	h.renderTemplate(w, "view.html", viewData{
		User:       claims,
		Image:      img,
		IsOwner:    img.OwnerID == claims.UserID,
		CurrentURL: h.cfg.Server.Domain + "/" + id,
	})
}
