package middleware

import (
	"net/http"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
)

// RequireUIAccess returns a middleware that gates access to UI pages.
//
// Authenticated requests (any non-nil Identity) pass through unconditionally.
// For unauthenticated requests:
//  1. OIDC enabled → 302 redirect to /auth/login with the current path as ?rd=.
//     By the time this middleware runs, DomainRedirect has already ensured the
//     request is on the canonical domain, so the session cookie will be set
//     on the right domain after login.
//  2. Otherwise → deniedHandler is invoked (caller controls the response).
func RequireUIAccess(cfg *config.Config, deniedHandler http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth.FromContext(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			if cfg.OIDC.Enabled {
				auth.LoginRedirect(w, r)
				return
			}
			deniedHandler.ServeHTTP(w, r)
		})
	}
}
