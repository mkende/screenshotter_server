package auth

import (
	"fmt"
	"net"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/config"
)

// tailscaleService extracts user identity from Tailscale-injected headers.
type tailscaleService struct {
	// allowedNets is the list of trusted source CIDRs (from server.trusted_proxy_ips).
	// Empty means trust all peer addresses.
	allowedNets []*net.IPNet
}

func newTailscaleService(cfg *config.Config) (*tailscaleService, error) {
	svc := &tailscaleService{}
	for _, cidr := range cfg.Server.TrustedProxyIPs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			// Already validated in config.Load, but be defensive.
			return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
		}
		svc.allowedNets = append(svc.allowedNets, network)
	}
	return svc, nil
}

// claimsFromHeaders reads Tailscale user headers and returns Claims, or nil
// if the request origin is not trusted or the headers are absent.
func (ts *tailscaleService) claimsFromHeaders(r *http.Request) *Claims {
	if len(ts.allowedNets) > 0 && !ts.peerAllowed(r) {
		return nil
	}
	login := r.Header.Get("Tailscale-User-Login")
	if login == "" {
		return nil
	}
	name := r.Header.Get("Tailscale-User-Name")
	if name == "" {
		name = login
	}
	return &Claims{
		UserID:      login,
		DisplayName: name,
		Email:       login,
	}
}

// peerAllowed reports whether the raw TCP peer IP (before any X-Forwarded-For
// rewriting) is in one of the allowed networks.
func (ts *tailscaleService) peerAllowed(r *http.Request) bool {
	ip := net.ParseIP(peerIPFromRequest(r))
	if ip == nil {
		return false
	}
	for _, network := range ts.allowedNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
