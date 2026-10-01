package middleware

import (
	"net/http"
	"net/url"
	"regexp"

	"github.com/mkende/screenshotter_server/internal/config"
	"github.com/mkende/screenshotter_server/internal/httputil"
)

// firefoxExtensionOrigin matches the origin of a Firefox extension's pages:
// moz-extension:// and the UUID Firefox picks at random for each
// installation of the extension, in the lowercase form it sends.
var firefoxExtensionOrigin = regexp.MustCompile(
	`^moz-extension://[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ExtensionOriginMatcher returns a function reporting whether an Origin
// header is that of an extension allowed to upload: a Chrome extension listed
// in cors.extension_ids, or, with cors.allow_firefox_extensions, any Firefox
// extension.
//
// Firefox extensions cannot be told apart by their origin: its host is a
// random UUID that the same extension gets anew on every installation, so
// the server can only trust them all. No website can send such an Origin, so
// the CSRF protection it is used for still holds; what it gives up is
// keeping out the user's other Firefox extensions.
func ExtensionOriginMatcher(cfg *config.Config) func(origin string) bool {
	chrome := make(map[string]struct{}, len(cfg.CORS.ExtensionIDs))
	for _, id := range cfg.CORS.ExtensionIDs {
		chrome["chrome-extension://"+id] = struct{}{}
	}
	allowFirefox := cfg.CORS.AllowFirefoxExtensions

	return func(origin string) bool {
		if _, ok := chrome[origin]; ok {
			return true
		}
		return allowFirefox && firefoxExtensionOrigin.MatchString(origin)
	}
}

// RequireSameOriginOrExtension returns middleware that rejects requests whose
// Origin header is neither the server's canonical address nor that of an
// allowed extension (see ExtensionOriginMatcher).
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
	isExtension := ExtensionOriginMatcher(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				httputil.WriteJSONError(w, http.StatusForbidden, "missing Origin header")
				return
			}
			if (cfg.CanonicalAddress != "" && origin == cfg.CanonicalAddress) || isExtension(origin) {
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
