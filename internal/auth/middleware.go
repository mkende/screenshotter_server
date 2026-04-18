package auth

import (
	"context"
	"encoding/json"
	"net/http"
)

// Middleware returns an http.Handler that enforces authentication.
// For browser requests it redirects to /auth/login on failure.
// For API requests (Accept: application/json or Upload endpoint) it returns 401.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := s.authenticate(r)
		if claims == nil {
			if isAPIRequest(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
					"login_url": s.cfg.Server.Domain + "/auth/login",
				})
			} else {
				http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			}
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalMiddleware sets claims in the request context when the request is
// authenticated, but always calls next regardless. Use this for routes that
// serve different content to logged-in vs. logged-out users.
func (s *Service) OptionalMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims := s.authenticate(r); claims != nil {
			r = r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate resolves claims from the request using whichever backend is active.
func (s *Service) authenticate(r *http.Request) *Claims {
	if s.anonymousSvc {
		return anonymousClaims
	}
	if s.tsSvc != nil {
		return s.tsSvc.claimsFromHeaders(r)
	}
	return s.parseSessionCookie(r)
}

// isAPIRequest returns true for requests that should get JSON error responses.
func isAPIRequest(r *http.Request) bool {
	return r.URL.Path == "/upload" || r.Header.Get("Accept") == "application/json"
}
