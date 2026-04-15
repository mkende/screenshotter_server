package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkende/screenshotter/server/internal/auth"
)

type updateRequest struct {
	// Title is the new title. Empty string clears the title (sets it to nil).
	Title     string `json:"title"`
	SourceURL string `json:"source_url"`
}

// Update handles PATCH /{id}: updates the title and/or source URL of an image
// owned by the authenticated user.
func (h *Handlers) Update(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.SourceURL = strings.TrimSpace(req.SourceURL)
	if req.SourceURL == "" {
		writeJSONError(w, http.StatusBadRequest, "source_url must not be empty")
		return
	}

	// A blank title means "no title set"; a non-empty title is stored as-is.
	req.Title = strings.TrimSpace(req.Title)
	var title *string
	if req.Title != "" {
		title = &req.Title
	}

	updated, err := h.db.UpdateImage(r.Context(), id, claims.UserID, title, req.SourceURL)
	if err != nil {
		slog.Error("update image", "id", id, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !updated {
		writeJSONError(w, http.StatusNotFound, "image not found or not owned by you")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{})
}
