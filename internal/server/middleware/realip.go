package middleware

import (
	"net"
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
	nets := auth.TrustedProxyNets(cfg)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(nets) > 0 {
				if ip := auth.PeerIP(r); ip != nil && auth.IPInRanges(ip, nets) {
					if realIP := extractForwardedIP(r, nets); realIP != "" {
						r.RemoteAddr = realIP
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractForwardedIP returns the originating client IP as reported by the
// proxy chain, or "" if no usable forwarding header is set.
//
// X-Forwarded-For is treated as authoritative: each well-behaved proxy appends
// the address it received the connection from, so the chain reads
// left-to-right as [spoofable client input..., real client, proxy1, proxy2].
// We scan right-to-left and return the first entry that is NOT itself a trusted
// proxy. With a single proxy hop this is the rightmost entry; with several it
// correctly skips the internal hops and stops at the real client, while any
// client-prepended values stay to the left of the real client and are never
// reached. This makes the value unspoofable as long as every proxy hop's
// address is listed in trusted_proxy.
//
// When X-Forwarded-For is absent (or contains only trusted addresses) we fall
// back to the single-value headers a trusted proxy may set instead.
func extractForwardedIP(r *http.Request, trustedNets []*net.IPNet) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			parsed := net.ParseIP(ip)
			if parsed == nil {
				continue
			}
			if auth.IPInRanges(parsed, trustedNets) {
				continue
			}
			return ip
		}
	}
	if ip := strings.TrimSpace(r.Header.Get("True-Client-IP")); ip != "" {
		return ip
	}
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	return ""
}
