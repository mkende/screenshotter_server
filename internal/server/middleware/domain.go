package middleware

import (
	"net"
	"net/http"
	"net/url"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
)

// NoRedirectParam is the query parameter that opts a request out of the
// canonical-address redirect. Clients that reach the server on a non-public
// address (the Slack bridge fetching over the intranet, health checks, curl
// against the listen address) add "?no_redirect=1" to be served in place
// instead of receiving a 301 to a host they may not be able to reach.
const NoRedirectParam = "no_redirect"

// SkipCanonicalRedirect reports whether r asks not to be redirected to the
// canonical address, i.e. carries no_redirect=1 in its query string.
func SkipCanonicalRedirect(r *http.Request) bool {
	return r.URL.Query().Get(NoRedirectParam) == "1"
}

// RedirectToCanonical checks whether r is already on the canonical address
// configured in cfg. If not, it writes a 301 redirect and returns true. If no
// canonical address is configured, the request already matches it, or the
// request opts out with no_redirect=1, it returns false and leaves w
// untouched.
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

	// The request explicitly asked to be served here rather than redirected.
	// The query is only parsed on this path, so the common (already
	// canonical) case stays free of the cost.
	if SkipCanonicalRedirect(r) {
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
