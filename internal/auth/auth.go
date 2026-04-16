// Package auth handles session cookies (JWT), OIDC login flow, and Tailscale
// header-based authentication. Only one backend is active at runtime.
package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/config"
)

// contextKey is an unexported type for context values set by this package.
type contextKey int

const (
	claimsKey  contextKey = iota
	peerIPKey             // raw TCP peer IP, set before any header-based rewriting
)

// WithPeerIP stores the raw TCP peer IP in r's context and returns the updated
// request.  Called by the server's real-IP middleware so that the Tailscale
// auth backend can check the actual peer regardless of X-Forwarded-For
// rewriting.
func WithPeerIP(r *http.Request, ip string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), peerIPKey, ip))
}

// peerIPFromRequest returns the raw TCP peer IP stored by WithPeerIP, falling
// back to parsing r.RemoteAddr if the context value is absent.
func peerIPFromRequest(r *http.Request) string {
	if v, ok := r.Context().Value(peerIPKey).(string); ok {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Service is the central auth object. Create via New.
type Service struct {
	cfg         *config.Config
	signingKey  []byte
	oidcSvc     *oidcService      // non-nil when backend == "oidc"
	tsSvc       *tailscaleService // non-nil when backend == "tailscale"
	anonymousSvc bool             // true when backend == "anonymous"
}

// anonymousClaims is returned for every request in anonymous mode.
var anonymousClaims = &Claims{
	UserID:      "anonymous",
	DisplayName: "Anonymous",
	Email:       "anonymous@localhost",
}

// New initialises the auth service. For OIDC, it contacts the identity
// provider's discovery endpoint, so ctx must have a usable timeout.
func New(ctx context.Context, cfg *config.Config) (*Service, error) {
	s := &Service{
		cfg:        cfg,
		signingKey: []byte(cfg.Session.Secret),
	}
	switch cfg.Auth.Backend {
	case "oidc":
		svc, err := newOIDCService(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("oidc init: %w", err)
		}
		s.oidcSvc = svc
	case "tailscale":
		svc, err := newTailscaleService(cfg)
		if err != nil {
			return nil, fmt.Errorf("tailscale init: %w", err)
		}
		s.tsSvc = svc
	case "anonymous":
		s.anonymousSvc = true
	}
	return s, nil
}

// ClaimsFromContext retrieves the authenticated user's claims from ctx.
// Returns nil if there is no authenticated user.
func ClaimsFromContext(ctx context.Context) *Claims {
	v, _ := ctx.Value(claimsKey).(*Claims)
	return v
}
