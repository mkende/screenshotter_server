package middleware

import (
	"net/http"
	"net/url"

	"github.com/mkende/screenshotter/server/internal/config"
	"github.com/mkende/screenshotter/server/internal/httputil"
)

// RequireSameOriginOrExtension returns middleware that rejects requests whose
// Origin header is neither the server's canonical address nor one of the
// registered Chrome extension origins.
//
// This is CSRF protection for endpoints like /upload that accept a
// CORS-"simple" content type (multipart/form-data) and therefore do not
// trigger a preflight. Without this check, any third-party website could
// submit a form that causes the logged-in user's browser to POST to us with
// the session cookie attached (SameSite=None, required for the extension).
//
// Requests with no Origin header are also rejected. Modern browsers send
// Origin on every POST; non-browser clients that legitimately call this
// endpoint must set it explicitly.
func RequireSameOriginOrExtension(cfg *config.Config) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, 1+len(cfg.CORS.ExtensionIDs))
	if cfg.CanonicalAddress != "" {
		allowed[cfg.CanonicalAddress] = struct{}{}
	}
	for _, id := range cfg.CORS.ExtensionIDs {
		allowed["chrome-extension://"+id] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				httputil.WriteJSONError(w, http.StatusForbidden, "missing Origin header")
				return
			}
			if _, ok := allowed[origin]; ok {
				next.ServeHTTP(w, r)
				return
			}
			// Fall back to host comparison when no canonical address is
			// configured, so same-origin browser requests still succeed.
			if cfg.CanonicalAddress == "" {
				if u, err := url.Parse(origin); err == nil && u.Host != "" && u.Host == r.Host {
					next.ServeHTTP(w, r)
					return
				}
			}
			httputil.WriteJSONError(w, http.StatusForbidden, "cross-origin request not allowed")
		})
	}
}
