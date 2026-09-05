package auth

import (
	"context"
	"net"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/config"
)

// WithOriginalRemoteAddr returns a context carrying the raw TCP remote address.
// Must be called by PreserveRemoteAddr middleware before chi's RealIP runs so
// that CIDR-based auth checks inspect the actual connecting IP rather than a
// spoofable X-Forwarded-For value.
func WithOriginalRemoteAddr(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, origRemoteAddrKey, addr)
}

// OriginalRemoteAddr returns the raw TCP remote address stored in ctx, or "".
func OriginalRemoteAddr(ctx context.Context) string {
	v, _ := ctx.Value(origRemoteAddrKey).(string)
	return v
}

// ParseCIDRs parses a slice of CIDR strings, returning the corresponding
// network list.
func ParseCIDRs(cidrs []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		nets = append(nets, ipnet)
	}
	return nets, nil
}

// TrustedProxyNets returns cfg.TrustedProxy parsed as networks. Config
// validation already rejects invalid CIDRs, so a parse failure here is a
// programming error and panics. Returns an empty list when no proxies are
// configured.
func TrustedProxyNets(cfg *config.Config) []*net.IPNet {
	nets, err := ParseCIDRs(cfg.TrustedProxy)
	if err != nil {
		panic("invalid trusted_proxy in config: " + err.Error())
	}
	return nets
}

// IPInRanges reports whether ip falls within any of the provided networks.
func IPInRanges(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// PeerIP returns the connecting IP address. It prefers the original TCP
// address saved in context (before RealIP rewrote r.RemoteAddr), falling
// back to r.RemoteAddr when the context value is absent (e.g. in unit tests).
// The port suffix is stripped.
func PeerIP(r *http.Request) net.IP {
	addr := OriginalRemoteAddr(r.Context())
	if addr == "" {
		addr = r.RemoteAddr
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(host)
}
