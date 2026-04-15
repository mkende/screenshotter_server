package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/db"
)

type homeData struct {
	User     *auth.Claims
	Images   []db.Image
	HomeCols int
	// Pagination
	Page     int
	PrevPage int
	NextPage int
	HasPrev  bool
	HasNext  bool
}

// Home renders the authenticated user's screenshots with pagination.
func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())

	if err := h.upsertUser(r.Context(), claims); err != nil {
		slog.Error("upsert user on home", "user", claims.UserID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	pageSize := h.cfg.Home.PageSize
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		page = p
	}
	offset := (page - 1) * pageSize

	// Fetch one extra to detect whether a next page exists.
	imgs, err := h.db.ListRecentImages(r.Context(), claims.UserID, pageSize+1, offset)
	if err != nil {
		slog.Error("list images", "user", claims.UserID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hasNext := len(imgs) > pageSize
	if hasNext {
		imgs = imgs[:pageSize]
	}

	h.renderTemplate(w, "home.html", homeData{
		User:     claims,
		Images:   imgs,
		HomeCols: h.cfg.Home.Cols,
		Page:     page,
		PrevPage: page - 1,
		NextPage: page + 1,
		HasPrev:  page > 1,
		HasNext:  hasNext,
	})
}
