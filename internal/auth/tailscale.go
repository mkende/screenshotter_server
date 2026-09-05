package auth

import (
	"log/slog"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/config"
)

// TailscaleMiddleware reads Tailscale-User-* headers and populates the
// identity context. If the header is absent or Tailscale auth is disabled,
// the request passes through unchanged.
//
// Headers are only accepted from requests whose raw TCP peer address is in
// cfg.TrustedProxy; config validation guarantees that TrustedProxy is
// non-empty when Tailscale is enabled.
//
// Note: Tailscale only injects Tailscale-User-* headers when using
// `tailscale serve` in HTTP proxy mode. Plain TCP forwarding does not inject
// these headers.
func TailscaleMiddleware(cfg *config.Config, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	trustedNets := TrustedProxyNets(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Tailscale.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			login := r.Header.Get("Tailscale-User-Login")
			if login == "" {
				logger.DebugContext(r.Context(), "tailscale: no Tailscale-User-Login header; headers are only injected by `tailscale serve` HTTP proxy mode, not TCPForward")
				next.ServeHTTP(w, r)
				return
			}
			if len(trustedNets) > 0 {
				ip := PeerIP(r)
				if ip == nil || !IPInRanges(ip, trustedNets) {
					logger.DebugContext(r.Context(), "tailscale: request from untrusted IP, ignoring headers",
						"remote_ip", ip,
						"trusted_cidrs", cfg.TrustedProxy)
					next.ServeHTTP(w, r)
					return
				}
			}

			name := r.Header.Get("Tailscale-User-Name")
			if name == "" {
				name = login
			}
			id := &Identity{
				Email:       login,
				DisplayName: name,
				AvatarURL:   r.Header.Get("Tailscale-User-Profile-Pic"),
				Source:      AuthSourceTailscale,
			}
			id.IsAdmin = isAdmin(cfg, id)
			logger.DebugContext(r.Context(), "tailscale: identity established",
				"email", id.Email,
				"is_admin", id.IsAdmin)
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}
