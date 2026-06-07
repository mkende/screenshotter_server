package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

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
	// Require valid UTF-8 so the stored title (and the X-Screenshot-Title header
	// derived from it) is always decodable as Unicode. JSON decoding already
	// substitutes U+FFFD for invalid bytes, so this is a defensive guard that
	// also covers any future non-JSON write path.
	if !utf8.ValidString(req.Title) {
		writeJSONError(w, http.StatusBadRequest, "title is not valid UTF-8")
		return
	}
	var title *string
	if req.Title != "" {
		title = &req.Title
	}

	sourceURL, err := h.parseSourceURL(req.SourceURL)
	if err != nil {
		msg := "source_url uses a scheme that is not allowed"
		if errors.Is(err, errSourceURLNotUTF8) {
			msg = "source_url is not valid UTF-8"
		}
		writeJSONError(w, http.StatusBadRequest, msg)
		return
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
