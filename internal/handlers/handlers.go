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
	mw "github.com/mkende/screenshotter/server/internal/server/middleware"
	"github.com/mkende/screenshotter/server/internal/storage"
	"github.com/mkende/screenshotter/server/internal/version"
)

// Handlers holds shared dependencies for all HTTP handlers.
type Handlers struct {
	cfg     *config.Config
	db      *db.DB
	storage *storage.Storage
	tmpls   map[string]*template.Template
	fontTTF []byte
}

// New creates a Handlers instance.
func New(cfg *config.Config, database *db.DB, stor *storage.Storage, tmpls map[string]*template.Template, fontTTF []byte) *Handlers {
	return &Handlers{
		cfg:     cfg,
		db:      database,
		storage: stor,
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

// pageData is the common set of fields included on every rendered page. Page
// templates extend it by embedding PageData and adding their own fields.
type pageData struct {
	User        *auth.Identity
	OIDCEnabled bool
	Title       string
	Version     string
	// Nonce is the per-request CSP nonce. Every <script> tag in the templates
	// must carry nonce="{{.Nonce}}" for the script-src policy to permit it.
	Nonce string
}

// newPageData returns a pageData populated with the common fields.
func (h *Handlers) newPageData(r *http.Request) pageData {
	return pageData{
		User:        auth.FromContext(r.Context()),
		OIDCEnabled: h.cfg.OIDC.Enabled,
		Title:       h.cfg.Title,
		Version:     version.Version,
		Nonce:       mw.NonceFromContext(r.Context()),
	}
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
func (h *Handlers) upsertUser(ctx context.Context, id *auth.Identity) error {
	return h.db.UpsertUser(ctx, id.Email, id.DisplayName, id.AvatarURL)
}
