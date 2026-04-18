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
	identity := auth.FromContext(r.Context())
	if identity == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := chi.URLParam(r, "id")

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	var title *string
	if req.Title != "" {
		title = &req.Title
	}

	var sourceURL *string
	if s := strings.TrimSpace(req.SourceURL); s != "" {
		sourceURL = &s
	}

	updated, err := h.db.UpdateImage(r.Context(), id, identity.Email, title, sourceURL)
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
