package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter_server/internal/auth"
	"github.com/mkende/screenshotter_server/internal/db"
)

type viewData struct {
	pageData
	Image      *db.Image
	IsOwner    bool
	CurrentURL string
	ImageURL   string
}

// View renders the HTML view page for a single image. Accessible to
// unauthenticated users (view is public); only the owner sees edit/delete
// controls.
func (h *Handlers) View(w http.ResponseWriter, r *http.Request) {
	identity := auth.FromContext(r.Context())
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
	isOwner := identity != nil && img.OwnerID == identity.Email
	h.renderTemplate(w, "view.html", viewData{
		pageData:   h.newPageData(r),
		Image:      img,
		IsOwner:    isOwner,
		CurrentURL: h.cfg.CanonicalAddress + "/" + id,
		ImageURL:   h.cfg.CanonicalAddress + "/" + id + ".png",
	})
}
