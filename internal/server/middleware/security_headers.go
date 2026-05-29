package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/config"
)

type nonceKey struct{}

// NonceFromContext returns the per-request CSP nonce stored by SecurityHeaders.
// Returns "" if the middleware has not run (e.g. in tests that bypass it).
func NonceFromContext(ctx context.Context) string {
	v, _ := ctx.Value(nonceKey{}).(string)
	return v
}

// generateNonce returns a 128-bit random value encoded as base64. On the
// extremely rare crypto/rand failure it returns "" — the CSP then allows no
// inline scripts, which is secure but breaks the page UI.
func generateNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// SecurityHeaders returns middleware that adds defensive HTTP headers to every
// response. A fresh CSP nonce is generated per request and stored in the
// request context (retrieve with NonceFromContext); templates must emit it as a
// nonce attribute on every <script> tag. When cfg's canonical address uses
// HTTPS, Strict-Transport-Security is also set.
func SecurityHeaders(cfg *config.Config) func(http.Handler) http.Handler {
	httpsOnly := cfg != nil && cfg.CanonicalScheme() == "https"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nonce := generateNonce()
			ctx := context.WithValue(r.Context(), nonceKey{}, nonce)

			h := w.Header()
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self' 'nonce-"+nonce+"'; "+
					"style-src 'self' 'unsafe-inline'; "+
					"img-src 'self' data: blob:; "+
					"connect-src 'self'; "+
					"font-src 'self' data:; "+
					"object-src 'none'; "+
					"base-uri 'self'; "+
					"form-action 'self'; "+
					"frame-ancestors 'none'")
			if httpsOnly {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
