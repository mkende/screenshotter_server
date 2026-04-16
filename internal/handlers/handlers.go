// Package handlers contains all HTTP handler methods for the screenshotter server.
package handlers

import (
	"context"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
	"github.com/mkende/screenshotter/server/internal/db"
	"github.com/mkende/screenshotter/server/internal/storage"
)

// Handlers holds shared dependencies for all HTTP handlers.
type Handlers struct {
	cfg     *config.Config
	db      *db.DB
	storage *storage.Storage
	auth    *auth.Service
	tmpls   map[string]*template.Template
	fontTTF []byte
}

// New creates a Handlers instance.
func New(cfg *config.Config, database *db.DB, stor *storage.Storage, authSvc *auth.Service, tmpls map[string]*template.Template, fontTTF []byte) *Handlers {
	return &Handlers{
		cfg:     cfg,
		db:      database,
		storage: stor,
		auth:    authSvc,
		tmpls:   tmpls,
		fontTTF: fontTTF,
	}
}

// ServeFont serves the embedded Roboto Regular TTF font used by the annotation editor.
func (h *Handlers) ServeFont(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "font/ttf")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(h.fontTTF) //nolint:errcheck
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "err", err)
	}
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// renderTemplate executes the named page template, writing 500 on failure.
// The base layout is always rendered via ExecuteTemplate(w, "base", data).
func (h *Handlers) renderTemplate(w http.ResponseWriter, page string, data any) {
	t, ok := h.tmpls[page]
	if !ok {
		slog.Error("template not found", "page", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("render template", "page", page, "err", err)
		// Headers may already be sent; best-effort log only.
	}
}

// upsertUser ensures the authenticated user exists in the DB.
// Required for the Tailscale backend, which has no explicit login step.
func (h *Handlers) upsertUser(ctx context.Context, claims *auth.Claims) error {
	return h.db.UpsertUser(ctx, claims.UserID, claims.DisplayName, claims.Email)
}
