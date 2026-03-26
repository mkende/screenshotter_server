package auth

import (
	"fmt"
	"net"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/config"
)

// tailscaleService extracts user identity from Tailscale-injected headers.
type tailscaleService struct {
	// allowedNets is the list of trusted source CIDRs. Empty means trust all.
	allowedNets []*net.IPNet
}

func newTailscaleService(cfg *config.Config) (*tailscaleService, error) {
	svc := &tailscaleService{}
	for _, cidr := range cfg.Auth.Tailscale.ProxyIPs {
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
	if len(ts.allowedNets) > 0 && !ts.remoteAddrAllowed(r) {
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

// remoteAddrAllowed reports whether the request's remote address is in one of
// the allowed networks. It handles both IPv4 and IPv6, and strips the port.
func (ts *tailscaleService) remoteAddrAllowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
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
