package middleware

import (
	"net/http"

	"github.com/mkende/screenshotter/server/internal/config"
)

// cspPolicy is the Content-Security-Policy applied to HTML responses.
//
// Screenshotter pulls Bulma CSS and Font Awesome from CDN mirrors for the UI,
// and the annotation editor loads Fabric.js at runtime. Those origins are
// explicitly allowlisted here; nothing else is permitted. 'unsafe-inline' is
// kept for both style-src (Bulma ships inline styles) and script-src (the
// templates include small inline <script> blocks for the upload and edit
// flows). Inline event handlers are in use on a few templates, so the script
// policy has to tolerate them.
const cspPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; " +
	"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; " +
	"img-src 'self' data: blob:; " +
	"connect-src 'self'; " +
	"font-src 'self' data: https://cdnjs.cloudflare.com; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'self'"

// SecurityHeaders returns middleware that adds defensive HTTP headers to every
// response. When cfg's canonical address uses HTTPS, Strict-Transport-Security
// is also set; the header is intentionally omitted for plain-HTTP deployments
// (Tailscale, local dev).
func SecurityHeaders(cfg *config.Config) func(http.Handler) http.Handler {
	httpsOnly := cfg != nil && cfg.CanonicalScheme() == "https"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", cspPolicy)
			if httpsOnly {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
