// Package handlers contains all HTTP handler methods for the screenshotter server.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
	"github.com/mkende/screenshotter/server/internal/db"
	mw "github.com/mkende/screenshotter/server/internal/server/middleware"
	"github.com/mkende/screenshotter/server/internal/storage"
	"github.com/mkende/screenshotter/server/internal/version"
)

// errSourceURLScheme is returned by parseSourceURL when a non-empty source URL
// uses a scheme that is not in the configured allowlist.
var errSourceURLScheme = errors.New("source_url scheme is not allowed")

// errSourceURLNotUTF8 is returned by parseSourceURL when a non-empty source URL
// is not valid UTF-8. We require valid UTF-8 so the stored value (and the
// X-Screenshot-Source-Url response header derived from it) is always decodable
// as Unicode.
var errSourceURLNotUTF8 = errors.New("source_url is not valid UTF-8")

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

// parseSourceURL trims raw and validates it. It returns (nil, nil) for an empty
// value (which clears the field), (ptr, nil) for an allowed URL,
// (nil, errSourceURLNotUTF8) when raw is non-empty but not valid UTF-8, or
// (nil, errSourceURLScheme) when its scheme is not in the configured allowlist
// (config.SourceURLSchemes). Because config validation strips dangerous schemes
// from the allowlist, those can never pass here regardless of input.
func (h *Handlers) parseSourceURL(raw string) (*string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	if !utf8.ValidString(s) {
		return nil, errSourceURLNotUTF8
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		return nil, errSourceURLScheme
	}
	scheme := strings.ToLower(u.Scheme)
	for _, allowed := range h.cfg.SourceURLSchemes {
		if scheme == allowed {
			return &s, nil
		}
	}
	return nil, errSourceURLScheme
}
