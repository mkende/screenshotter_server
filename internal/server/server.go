// Package server wires together the HTTP router, middleware, and handlers.
package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
	"github.com/mkende/screenshotter/server/internal/handlers"
	"github.com/mkende/screenshotter/server/internal/ratelimit"
	mw "github.com/mkende/screenshotter/server/internal/server/middleware"
	"github.com/mkende/screenshotter/server/internal/static"
)

// New builds and returns the main HTTP handler.
//
// oidcHandler may be nil when OIDC is not enabled; in that case the /auth/*
// routes respond with 404.
func New(cfg *config.Config, h *handlers.Handlers, oidcHandler *auth.OIDCHandler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	r := chi.NewRouter()

	// Order is critical here:
	//   1. PreserveRemoteAddr saves the raw TCP peer before RealIP rewrites it.
	//   2. TrustedRealIP rewrites r.RemoteAddr from forwarded headers, but
	//      only when the TCP peer is in trusted_proxy. Direct-internet
	//      clients cannot spoof their source IP.
	//   3. RequestLogger opens a log span and allocates RequestAttrs so later
	//      middleware can enrich the line.
	//   4. Recoverer catches panics from subsequent middleware and handlers.
	//   5. DomainRedirect issues 301 to the canonical address.
	//   6. SecurityHeaders attaches defensive response headers.
	//   7. CORS answers extension preflights and sets credential headers.
	//   8. Auth providers: Tailscale → ProxyAuth → OIDC session → Anonymous.
	//      Each no-ops when disabled or when a prior provider identified the
	//      user.
	//   9. LogEnricher reads the identity and fills request-scoped logger +
	//      log attrs.
	r.Use(mw.PreserveRemoteAddr)
	r.Use(mw.TrustedRealIP(cfg))
	r.Use(chimw.RequestID)
	r.Use(mw.RequestLogger(logger))
	r.Use(chimw.Recoverer)
	// CORS before DomainRedirect so that preflight OPTIONS from the Chrome
	// extension receives a proper 204 with headers even when the request
	// arrives on a non-canonical host.
	r.Use(corsMiddleware(cfg))
	r.Use(mw.DomainRedirect(cfg))
	r.Use(mw.SecurityHeaders(cfg))

	r.Use(auth.TailscaleMiddleware(cfg, logger))
	r.Use(auth.ProxyAuthMiddleware(cfg, logger))
	r.Use(auth.OIDCMiddleware(cfg, logger))
	r.Use(auth.AnonymousMiddleware(cfg, logger))

	r.Use(mw.LogEnricher(logger))

	rl := ratelimit.NewMemory(ratelimit.Config{
		RequestsPerSecond: cfg.RateLimit.RequestsPerSecond,
		RequestsPerMinute: cfg.RateLimit.RequestsPerMinute,
	})
	rlMiddleware := ratelimit.Middleware(rl)

	// Favicon — served directly from disk; no auth required.
	if faviconFile := resolveFaviconPath(cfg); faviconFile != "" {
		r.Get("/favicon.ico", faviconHandler(faviconFile))
	}

	// Static assets (CSS, JS, webfonts) — embedded in the binary, no auth required.
	r.Handle("/assets/*", http.StripPrefix("/assets", http.FileServerFS(static.Files)))

	// OIDC routes: present only when OIDC is enabled. The middleware stack
	// (DomainRedirect, SecurityHeaders, CORS, auth providers) still runs so
	// that /auth/callback is reached only on the canonical domain.
	if cfg.OIDC.Enabled && oidcHandler != nil {
		r.Get("/auth/login", oidcHandler.HandleLogin)
		r.Get("/auth/callback", oidcHandler.HandleCallback)
		r.Get("/auth/logout", oidcHandler.HandleLogout)
	}

	// Home page: optional auth — logged-in users see their gallery; others see
	// the logged-out landing page.
	r.Get("/", h.Home)

	// Image view routes. GET /{id} and GET /{id}.png are rate-limited — the
	// limit applies to every request including 404s to prevent ID enumeration
	// by unauthenticated scrapers. When Server.RequireAuthToView is set,
	// these routes additionally require an authenticated session.
	r.Group(func(r chi.Router) {
		r.Use(rlMiddleware)
		if cfg.Server.RequireAuthToView {
			r.Use(auth.RequireAuth(cfg))
		}
		r.Get(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.View)
		r.Get(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}.png", cfg.ID.Length), h.ServeImage)
	})

	// Routes that require authentication.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(cfg))

		// /upload accepts multipart/form-data (a CORS "simple" content type
		// that is not preflighted), so the server must verify the Origin
		// header to block CSRF from third-party sites exploiting the
		// SameSite=None session cookie.
		r.With(mw.RequireSameOriginOrExtension(cfg)).Post("/upload", h.Upload)
		r.Get("/static/font.ttf", h.ServeFont)

		r.Patch(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.Update)
		r.Delete(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.Delete)
		r.Get(fmt.Sprintf("/thumb/{id:[a-zA-Z0-9]{%d,}}.png", cfg.ID.Length), h.ServeThumb)
		r.Get(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}/annotate", cfg.ID.Length), h.AnnotateView)
		r.Post(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}/annotate", cfg.ID.Length), h.Annotate)
	})

	return r
}

// resolveFaviconPath returns the absolute favicon path, preferring
// cfg.FaviconPath over cfg.Server.AssetsPath/favicon.ico. Returns "" if
// neither is configured.
func resolveFaviconPath(cfg *config.Config) string {
	if cfg.FaviconPath != "" {
		return cfg.FaviconPath
	}
	if cfg.Server.AssetsPath != "" {
		return filepath.Join(cfg.Server.AssetsPath, "favicon.ico")
	}
	return ""
}

// faviconHandler serves favicon.ico from path.
func faviconHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/x-icon")
		http.ServeContent(w, r, "favicon.ico", stat.ModTime(), f)
	}
}

// corsMiddleware sets CORS headers for requests from registered extension
// origins. Non-matching origins are passed through unchanged, which is
// important because the Chrome extension's Origin header starts with
// chrome-extension:// and same-origin browser requests have no Origin.
func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(cfg.CORS.ExtensionIDs))
	for _, id := range cfg.CORS.ExtensionIDs {
		allowed["chrome-extension://"+id] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")

				if r.Method == http.MethodOptions {
					w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", strings.Join([]string{
						"Content-Type", "Accept",
					}, ", "))
					w.Header().Set("Access-Control-Max-Age", "86400")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
