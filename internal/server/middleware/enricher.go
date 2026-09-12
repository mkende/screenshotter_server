package middleware

import (
	"net/http"

	"github.com/mkende/screenshotter_server/internal/auth"
)

// LogEnricher returns a middleware that must run after all authentication
// middlewares. It reads the identity (if any) from context and fills the
// RequestAttrs allocated by RequestLogger with the auth source and the
// request's Host, so the deferred request log line includes them.
func LogEnricher() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a := RequestAttrsFromContext(r.Context()); a != nil {
				if id := auth.FromContext(r.Context()); id != nil {
					a.AuthSource = string(id.Source)
				}
				a.Domain = r.Host
			}
			next.ServeHTTP(w, r)
		})
	}
}
