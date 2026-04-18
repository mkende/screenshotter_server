package middleware

import (
	"net/http"

	"github.com/mkende/screenshotter/server/internal/auth"
)

// PreserveRemoteAddr saves r.RemoteAddr into the request context before any
// RealIP middleware can overwrite it with the X-Forwarded-For value. The saved
// address is later read by CIDR-based auth middlewares (Tailscale, proxy auth)
// to verify that headers arrive from a trusted network range, using the actual
// TCP connection address rather than the spoofable forwarded header.
//
// This middleware must be registered before chi's middleware.RealIP.
func PreserveRemoteAddr(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithOriginalRemoteAddr(r.Context(), r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
