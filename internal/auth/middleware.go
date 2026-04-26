package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/mkende/screenshotter/server/internal/config"
)

// isAPIRequest reports whether the request is one that expects a JSON
// response on auth failure (401/403) rather than an HTML redirect.
//
// Screenshotter's Chrome extension posts to /upload and expects JSON. The
// annotation editor POSTs to /{id}/annotate with Content-Type: image/png and
// also expects JSON. Browser requests that render HTML should be redirected.
func isAPIRequest(r *http.Request) bool {
	if r.URL.Path == "/upload" {
		return true
	}
	if strings.HasSuffix(r.URL.Path, "/annotate") && r.Method == http.MethodPost {
		return true
	}
	// Any request that explicitly asks for JSON, or any non-safe method, is
	// treated as an API request.
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		return true
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// writeJSONError writes a JSON {"error": message} response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message}) //nolint:errcheck
}

// LoginRedirect redirects the user to the OIDC login page, encoding the
// current request URI as the ?rd= post-login destination.
func LoginRedirect(w http.ResponseWriter, r *http.Request) {
	loginURL := "/auth/login?rd=" + url.QueryEscape(r.URL.RequestURI())
	http.Redirect(w, r, loginURL, http.StatusFound)
}

// RequireAuth returns a middleware that enforces authentication on a route.
//
// For API-style requests an unauthenticated caller receives a 401 JSON
// response. For browser HTML requests, if OIDC is enabled the user is
// redirected to the login page; otherwise a 403 is written.
func RequireAuth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if FromContext(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			if isAPIRequest(r) {
				if cfg.OIDC.Enabled {
					loginURL := strings.TrimRight(cfg.CanonicalAddress, "/") + "/auth/login?rd=/auth/done"
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
						"error":     "authentication required",
						"login_url": loginURL,
					})
				} else {
					writeJSONError(w, http.StatusUnauthorized, "authentication required")
				}
				return
			}
			if cfg.OIDC.Enabled {
				LoginRedirect(w, r)
				return
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}

// RequireAdmin returns a middleware that enforces admin access. Non-admin
// requests are passed to deniedHandler (HTML) or receive a 403 JSON response
// (API).
func RequireAdmin(deniedHandler http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := FromContext(r.Context())
			if id != nil && id.IsAdmin {
				next.ServeHTTP(w, r)
				return
			}
			if isAPIRequest(r) {
				writeJSONError(w, http.StatusForbidden, "admin privileges required")
				return
			}
			deniedHandler.ServeHTTP(w, r)
		})
	}
}
