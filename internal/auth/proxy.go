package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/mkende/screenshotter/server/internal/config"
)

// ProxyAuthMiddleware reads forward-auth headers injected by a trusted
// reverse proxy (nginx, Caddy, Traefik, Authelia, …) and populates the
// identity context. No-op when proxy auth is disabled, when the request
// originates from an untrusted IP, or when another middleware has already
// established an identity.
//
// Header names default to the de-facto standard used by Authelia:
//
//   - Remote-User   — username / login name (fallback identifier)
//   - Remote-Email  — email address (preferred primary identifier)
//   - Remote-Name   — display name
//   - Remote-Groups — comma-separated group memberships
func ProxyAuthMiddleware(cfg *config.Config, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	trustedNets := TrustedProxyNets(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.ProxyAuth.Enabled || FromContext(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			ip := PeerIP(r)
			if ip == nil || !IPInRanges(ip, trustedNets) {
				logger.DebugContext(r.Context(), "proxy_auth: request from untrusted IP, ignoring headers",
					"remote_ip", ip,
					"trusted_cidrs", cfg.TrustedProxy)
				next.ServeHTTP(w, r)
				return
			}

			// Prefer the dedicated email header; fall back to the user header.
			email := r.Header.Get(cfg.ProxyAuth.EmailHeader)
			if email == "" {
				email = r.Header.Get(cfg.ProxyAuth.UserHeader)
			}
			if email == "" {
				logger.DebugContext(r.Context(), "proxy_auth: trusted IP but no identity headers present",
					"remote_ip", ip,
					"email_header", cfg.ProxyAuth.EmailHeader,
					"user_header", cfg.ProxyAuth.UserHeader)
				next.ServeHTTP(w, r)
				return
			}

			id := &Identity{
				Email:       email,
				DisplayName: r.Header.Get(cfg.ProxyAuth.NameHeader),
				Source:      AuthSourceProxy,
			}
			if raw := r.Header.Get(cfg.ProxyAuth.GroupsHeader); raw != "" {
				id.Groups = splitGroups(raw)
			}
			id.IsAdmin = isAdmin(cfg, id)
			logger.DebugContext(r.Context(), "proxy_auth: identity established",
				"email", id.Email,
				"is_admin", id.IsAdmin)
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

// splitGroups splits a comma-separated groups string, trimming whitespace and
// omitting empty entries.
func splitGroups(raw string) []string {
	parts := strings.Split(raw, ",")
	groups := make([]string, 0, len(parts))
	for _, p := range parts {
		if g := strings.TrimSpace(p); g != "" {
			groups = append(groups, g)
		}
	}
	return groups
}
