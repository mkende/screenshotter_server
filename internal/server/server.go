// Package server wires together the HTTP router, middleware, and handlers.
package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
	"github.com/mkende/screenshotter/server/internal/handlers"
)

// New builds and returns the main HTTP handler.
func New(cfg *config.Config, h *handlers.Handlers, authSvc *auth.Service) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(cfg))

	// Auth routes (no session required).
	r.Get("/auth/login", h.LoginHandler)
	r.Get("/auth/callback", h.CallbackHandler)
	r.Get("/auth/logout", h.LogoutHandler)

	// All other routes require authentication.
	r.Group(func(r chi.Router) {
		r.Use(authSvc.Middleware)

		r.Get("/", h.Home)
		r.Post("/upload", h.Upload)

		// Image routes: alphanumeric IDs only.
		r.Get(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.View)
		r.Get(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}.png", cfg.ID.Length), h.ServeImage)
		r.Patch(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.Update)
		r.Delete(fmt.Sprintf("/{id:[a-zA-Z0-9]{%d,}}", cfg.ID.Length), h.Delete)
		r.Get(fmt.Sprintf("/thumb/{id:[a-zA-Z0-9]{%d,}}.png", cfg.ID.Length), h.ServeThumb)
	})

	return r
}

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
