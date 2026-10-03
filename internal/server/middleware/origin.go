package middleware

import (
	"net/http"
	"net/url"
	"regexp"

	"github.com/mkende/screenshotter_server/internal/config"
	"github.com/mkende/screenshotter_server/internal/httputil"
)

// extensionOrigin matches the origin of a browser extension's pages: a
// Chrome (or Chromium-based browser's) extension, chrome-extension:// and its
// 32-letter ID, or a Firefox one, moz-extension:// and the UUID Firefox picks
// at random for each installation of the extension, in the lowercase form it
// sends.
var extensionOrigin = regexp.MustCompile(
	`^(chrome-extension://[a-p]{32}|moz-extension://[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

// IsExtensionOrigin reports whether an Origin header is that of a browser
// extension, which may upload and make other credentialed requests.
//
// Any extension qualifies, not only Screenshotter. No website can send such
// an Origin, which is what the CSRF protection relies on. Telling our
// extension apart from the user's others is not something the Origin can
// do: Firefox gives each installation of an extension a random one, and in
// Chrome any extension with host access to the server bypasses CORS anyway.
func IsExtensionOrigin(origin string) bool {
	return extensionOrigin.MatchString(origin)
}

// RequireSameOriginOrExtension returns middleware that rejects requests whose
// Origin header is neither the server's canonical address nor that of a
// browser extension (see IsExtensionOrigin).
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
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				httputil.WriteJSONError(w, http.StatusForbidden, "missing Origin header")
				return
			}
			if (cfg.CanonicalAddress != "" && origin == cfg.CanonicalAddress) || IsExtensionOrigin(origin) {
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
