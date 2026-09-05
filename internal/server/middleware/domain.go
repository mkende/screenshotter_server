package middleware

import (
	"net"
	"net/http"
	"net/url"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
)

// RedirectToCanonical checks whether r is already on the canonical address
// configured in cfg. If not, it writes a 301 redirect and returns true. If no
// canonical address is configured, or the request already matches it, it
// returns false and leaves w untouched.
//
// trustedNets is used to determine whether to trust X-Forwarded-Proto: the
// header is only honoured when the peer IP falls within one of those ranges.
func RedirectToCanonical(cfg *config.Config, trustedNets []*net.IPNet, w http.ResponseWriter, r *http.Request) bool {
	canonicalScheme := cfg.CanonicalScheme()
	canonicalHost := cfg.CanonicalHost()
	if canonicalScheme == "" || canonicalHost == "" {
		return false
	}

	// Determine the effective scheme of the incoming request. Trust
	// X-Forwarded-Proto only when the peer IP is in trusted_proxy.
	reqScheme := "http"
	if r.TLS != nil {
		reqScheme = "https"
	} else if len(trustedNets) > 0 {
		if ip := auth.PeerIP(r); ip != nil && auth.IPInRanges(ip, trustedNets) {
			if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
				reqScheme = proto
			}
		}
	}

	if reqScheme == canonicalScheme && r.Host == canonicalHost {
		return false
	}

	target := &url.URL{
		Scheme:   canonicalScheme,
		Host:     canonicalHost,
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
	}
	http.Redirect(w, r, target.String(), http.StatusMovedPermanently)
	return true
}

// DomainRedirect returns middleware that redirects any request not on the
// canonical address to that address (301), preserving path and query.
func DomainRedirect(cfg *config.Config) func(http.Handler) http.Handler {
	trustedNets := auth.TrustedProxyNets(cfg)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !RedirectToCanonical(cfg, trustedNets, w, r) {
				next.ServeHTTP(w, r)
			}
		})
	}
}
