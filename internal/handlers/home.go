package handlers

import (
	"log/slog"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/db"
)

type homeData struct {
	User   *auth.Claims
	Images []db.Image
}

// Home renders the authenticated user's last 20 uploaded images.
func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())

	// Ensure the user record exists (needed for Tailscale backend where there
	// is no explicit login step that would call UpsertUser).
	if err := h.upsertUser(r.Context(), claims); err != nil {
		slog.Error("upsert user on home", "user", claims.UserID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	imgs, err := h.db.ListRecentImages(r.Context(), claims.UserID, 20)
	if err != nil {
		slog.Error("list images", "user", claims.UserID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.renderTemplate(w, "home.html", homeData{User: claims, Images: imgs})
}
