// Package server wires together the HTTP router, middleware, and handlers.
package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
	"github.com/mkende/screenshotter/server/internal/handlers"
	"github.com/mkende/screenshotter/server/internal/ratelimit"
)

// New builds and returns the main HTTP handler.
func New(cfg *config.Config, h *handlers.Handlers, authSvc *auth.Service) http.Handler {
	r := chi.NewRouter()

	// Parse trusted proxy CIDRs once at startup.
	trustedNets := parseCIDRs(cfg.Server.TrustedProxyIPs)

	r.Use(realIPMiddleware(trustedNets))
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(cfg))

	// Rate limiting applies to every route, including 404s on image paths,
	// to prevent enumeration of image IDs.
	rl := ratelimit.NewMemory(ratelimit.Config{
		RequestsPerSecond: cfg.RateLimit.RequestsPerSecond,
		RequestsPerMinute: cfg.RateLimit.RequestsPerMinute,
	})
	r.Use(ratelimit.Middleware(rl))

	// Favicon — served directly from disk; no auth required.
	if cfg.Server.AssetsPath != "" {
		r.Get("/favicon.ico", faviconHandler(cfg.Server.AssetsPath))
	}

	// Auth routes (no session required).
	r.Get("/auth/login", h.LoginHandler)
	r.Get("/auth/callback", h.CallbackHandler)
	r.Get("/auth/logout", h.LogoutHandler)

	// All other routes require authentication.
	r.Group(func(r chi.Router) {
		r.Use(authSvc.Middleware)

		r.Get("/", h.Home)
		r.Post("/upload", h.Upload)
		r.Get("/static/font.ttf", h.ServeFont)

		// Image routes: alphanumeric IDs only.
		r.Get(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.View)
		r.Get(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}.png", cfg.ID.Length), h.ServeImage)
		r.Patch(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.Update)
		r.Delete(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.Delete)
		r.Get(fmt.Sprintf("/thumb/{id:[a-zA-Z0-9]{%d,}}.png", cfg.ID.Length), h.ServeThumb)
		r.Get(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}/annotate", cfg.ID.Length), h.AnnotateView)
		r.Post(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}/annotate", cfg.ID.Length), h.Annotate)
	})

	return r
}

// ── Favicon ───────────────────────────────────────────────────────────────────

// faviconHandler serves favicon.ico from assetsPath.
func faviconHandler(assetsPath string) http.HandlerFunc {
	faviconFile := filepath.Join(assetsPath, "favicon.ico")
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(faviconFile)
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

// ── Real-IP middleware ────────────────────────────────────────────────────────

// realIPMiddleware replaces chi's middleware.RealIP.  It stores the raw TCP
// peer IP in the request context (via auth.WithPeerIP so Tailscale auth can
// read it after any header-based rewriting), then rewrites r.RemoteAddr from
// X-Forwarded-For / X-Real-IP only when the peer is in trustedNets.
func realIPMiddleware(trustedNets []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := bareIP(r.RemoteAddr)

			// Always save the raw peer before any rewriting.
			r = auth.WithPeerIP(r, peer)

			if len(trustedNets) > 0 && ipInNets(peer, trustedNets) {
				if realIP := firstForwardedFor(r); realIP != "" {
					r.RemoteAddr = realIP
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// firstForwardedFor returns the leftmost (client) IP from X-Forwarded-For, or
// the value of X-Real-IP, whichever is present first (X-Forwarded-For wins).
// Returns "" if neither header is set or valid.
func firstForwardedFor(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For: client, proxy1, proxy2 — take the leftmost entry.
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			xff = strings.TrimSpace(xff[:idx])
		} else {
			xff = strings.TrimSpace(xff)
		}
		if net.ParseIP(xff) != nil {
			return xff
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}
	return ""
}

func bareIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			nets = append(nets, network)
		}
	}
	return nets
}

func ipInNets(ip string, nets []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// ── CORS middleware ───────────────────────────────────────────────────────────

// corsMiddleware sets CORS headers for requests from registered extension origins.
func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	// Build a set of allowed origins for O(1) lookup.
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
