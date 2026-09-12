// Package auth provides authentication middleware and handlers.
//
// Four independent auth providers can be enabled simultaneously: Tailscale
// header-based auth, reverse-proxy forward-auth headers, OIDC (with JWT
// session cookies), and a shared anonymous fallback. Each provider is a
// middleware that populates Identity on the request context when it succeeds;
// a later middleware (RequireAuth) enforces the presence of an identity on
// protected routes.
package auth

import (
	"context"

	"github.com/mkende/screenshotter_server/internal/config"
)

// AuthSource identifies how the user was authenticated.
type AuthSource string

const (
	// AuthSourceOIDC indicates authentication via OpenID Connect.
	AuthSourceOIDC AuthSource = "oidc"
	// AuthSourceTailscale indicates authentication via Tailscale headers.
	AuthSourceTailscale AuthSource = "tailscale"
	// AuthSourceProxy indicates authentication via reverse-proxy forward-auth headers.
	AuthSourceProxy AuthSource = "proxy"
	// AuthSourceAnonymous indicates the anonymous fallback identity.
	AuthSourceAnonymous AuthSource = "anonymous"
)

// Identity holds the authenticated user's information. Email is the canonical
// user key used throughout the application (stored as images.owner_id).
type Identity struct {
	Email       string
	DisplayName string
	AvatarURL   string
	Groups      []string
	IsAdmin     bool
	// Source identifies which authentication mechanism produced this identity.
	Source AuthSource
}

type contextKey int

const (
	identityKey contextKey = iota
	origRemoteAddrKey
)

// WithIdentity returns a new context carrying the given identity.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// FromContext returns the identity from ctx, or nil if not authenticated.
func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey).(*Identity)
	return id
}

// isAdmin reports whether the given identity has admin privileges according
// to the config's admin_emails list and admin_groups setting.
func isAdmin(cfg *config.Config, id *Identity) bool {
	for _, email := range cfg.AdminEmails {
		if email == id.Email {
			return true
		}
	}
	for _, adminGroup := range cfg.AdminGroups {
		for _, g := range id.Groups {
			if g == adminGroup {
				return true
			}
		}
	}
	return false
}
