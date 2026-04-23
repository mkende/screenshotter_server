package middleware

import (
	"net/http"
	"strings"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
)

// PreserveRemoteAddr saves r.RemoteAddr into the request context before any
// RealIP middleware can overwrite it with the X-Forwarded-For value. The saved
// address is later read by CIDR-based auth middlewares (Tailscale, proxy auth)
// to verify that headers arrive from a trusted network range, using the actual
// TCP connection address rather than the spoofable forwarded header.
//
// This middleware must be registered before TrustedRealIP.
func PreserveRemoteAddr(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithOriginalRemoteAddr(r.Context(), r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TrustedRealIP rewrites r.RemoteAddr from True-Client-IP / X-Real-IP /
// X-Forwarded-For, but only when the raw TCP peer address is within
// cfg.TrustedProxy. Direct-internet clients cannot spoof their source IP by
// setting these headers — the headers are ignored unless the request actually
// arrives from a trusted proxy. When no trusted proxies are configured, or
// when the peer is not trusted, r.RemoteAddr is left unchanged.
//
// PreserveRemoteAddr must run before this middleware so that the original TCP
// peer is preserved for auth providers that do CIDR checks.
func TrustedRealIP(cfg *config.Config) func(http.Handler) http.Handler {
	nets, err := auth.ParseCIDRs(cfg.TrustedProxy)
	if err != nil {
		panic("realip: invalid trusted_proxy in config: " + err.Error())
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(nets) > 0 {
				if ip := auth.PeerIP(r); ip != nil && auth.IPInRanges(ip, nets) {
					if realIP := extractForwardedIP(r); realIP != "" {
						r.RemoteAddr = realIP
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractForwardedIP returns the client IP as reported by the proxy, or "" if
// no forwarding header is set. Checks True-Client-IP, then X-Real-IP, then the
// leftmost entry of X-Forwarded-For.
func extractForwardedIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("True-Client-IP")); ip != "" {
		return ip
	}
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return ""
}
