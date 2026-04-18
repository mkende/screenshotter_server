package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/db"
)

type homeData struct {
	pageData
	Images   []db.Image
	HomeCols int
	Page     int
	PrevPage int
	NextPage int
	HasPrev  bool
	HasNext  bool
}

// Home renders the authenticated user's screenshots with pagination, or a
// logged-out landing page for unauthenticated visitors.
func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id == nil {
		h.renderTemplate(w, "home-loggedout.html", h.newPageData(r))
		return
	}

	if err := h.upsertUser(r.Context(), id); err != nil {
		slog.Error("upsert user on home", "email", id.Email, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	pageSize := h.cfg.Home.PageSize
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		page = p
	}
	offset := (page - 1) * pageSize

	imgs, err := h.db.ListRecentImages(r.Context(), id.Email, pageSize+1, offset)
	if err != nil {
		slog.Error("list images", "email", id.Email, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hasNext := len(imgs) > pageSize
	if hasNext {
		imgs = imgs[:pageSize]
	}

	h.renderTemplate(w, "home.html", homeData{
		pageData: h.newPageData(r),
		Images:   imgs,
		HomeCols: h.cfg.Home.Cols,
		Page:     page,
		PrevPage: page - 1,
		NextPage: page + 1,
		HasPrev:  page > 1,
		HasNext:  hasNext,
	})
}
