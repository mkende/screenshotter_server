package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/mkende/screenshotter_server/internal/config"
	"golang.org/x/oauth2"
)

// OIDC short-lived cookie names used during the login round-trip.
const (
	oidcStateCookie    = "oidc_state"
	oidcVerifierCookie = "oidc_verifier"
	oidcNonceCookie    = "oidc_nonce"
	oidcCookieMaxAge   = 600 // 10 minutes
)

// userUpserter is the minimal DB interface used by the OIDC callback to
// record users as they log in.
type userUpserter interface {
	UpsertUser(ctx context.Context, email, displayName, avatarURL string) error
}

// OIDCHandler handles the /auth/login, /auth/callback, and /auth/logout routes.
type OIDCHandler struct {
	cfg      *config.Config
	provider *gooidc.Provider
	oauth2   oauth2.Config
	verifier *gooidc.IDTokenVerifier
	users    userUpserter
}

// NewOIDCHandler creates a new OIDCHandler by contacting the OIDC provider's
// discovery endpoint. Pass nil for users to disable user upserting.
func NewOIDCHandler(ctx context.Context, cfg *config.Config, users userUpserter) (*OIDCHandler, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.OIDC.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	return &OIDCHandler{
		cfg:      cfg,
		provider: provider,
		oauth2: oauth2.Config{
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.CanonicalAddress + "/auth/callback",
			Scopes:       cfg.OIDC.Scopes,
		},
		verifier: provider.Verifier(&gooidc.Config{ClientID: cfg.OIDC.ClientID}),
		users:    users,
	}, nil
}

// HandleLogin redirects the browser to the OIDC provider, carrying a PKCE
// verifier (in a short-lived cookie) and encoding the desired post-login
// destination into the OAuth state parameter.
func (h *OIDCHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	random, err := randomB64(16)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Encode the post-login destination into the state so it survives the
	// round-trip to the OIDC provider. Format: "<random>|<rd>".
	state := random + "|" + safeRedirectPath(r.URL.Query().Get("rd"))

	setOIDCCookie(w, oidcStateCookie, state, oidcCookieMaxAge)

	// Bind the resulting id_token to this login request with a nonce (OIDC
	// Core). The nonce is stored in a short-lived cookie and verified against
	// the id_token's nonce claim in the callback.
	nonce, err := randomB64(16)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setOIDCCookie(w, oidcNonceCookie, nonce, oidcCookieMaxAge)

	authOpts := []oauth2.AuthCodeOption{gooidc.Nonce(nonce)}

	// Add PKCE support if enabled
	if h.cfg.OIDC.UsePKCE {
		verifier, challenge, err := pkce()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		setOIDCCookie(w, oidcVerifierCookie, verifier, oidcCookieMaxAge)
		authOpts = append(authOpts,
			oauth2.SetAuthURLParam("code_challenge", challenge),
			oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		)
	}

	http.Redirect(w, r, h.oauth2.AuthCodeURL(state, authOpts...), http.StatusFound)
}

// HandleCallback completes the OIDC authorization-code + PKCE exchange,
// issues a session JWT cookie, and redirects to the destination encoded in
// the state.
func (h *OIDCHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	clearOIDCCookie(w, oidcStateCookie)
	clearOIDCCookie(w, oidcVerifierCookie)
	clearOIDCCookie(w, oidcNonceCookie)

	// Get PKCE verifier if PKCE is enabled
	var verifier string
	if h.cfg.OIDC.UsePKCE {
		verifierCookie, err := r.Cookie(oidcVerifierCookie)
		if err != nil {
			http.Error(w, "missing PKCE verifier", http.StatusBadRequest)
			return
		}
		verifier = verifierCookie.Value
	}

	var token *oauth2.Token
	if verifier != "" {
		token, err = h.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"),
			oauth2.SetAuthURLParam("code_verifier", verifier))
	} else {
		token, err = h.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"))
	}
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token", http.StatusInternalServerError)
		return
	}
	idToken, err := h.verifier.Verify(r.Context(), rawID)
	if err != nil {
		http.Error(w, "id_token verification failed", http.StatusUnauthorized)
		return
	}

	// Verify the nonce binds this id_token to the login request we initiated.
	nonceCookie, err := r.Cookie(oidcNonceCookie)
	if err != nil || idToken.Nonce == "" || idToken.Nonce != nonceCookie.Value {
		http.Error(w, "invalid nonce", http.StatusBadRequest)
		return
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified *bool  `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "claims extraction failed", http.StatusInternalServerError)
		return
	}
	if claims.Email == "" {
		http.Error(w, "id_token missing email claim", http.StatusUnauthorized)
		return
	}
	if h.cfg.OIDC.RequireEmailVerified && (claims.EmailVerified == nil || !*claims.EmailVerified) {
		slog.WarnContext(r.Context(), "oidc: rejecting login with unverified email",
			"email", claims.Email,
			"email_verified_present", claims.EmailVerified != nil)
		http.Error(w, "email is not verified by the identity provider", http.StatusUnauthorized)
		return
	}
	var groups []string
	var raw map[string]json.RawMessage
	if err := idToken.Claims(&raw); err == nil {
		if gc, ok := raw[h.cfg.OIDC.GroupsClaim]; ok {
			_ = json.Unmarshal(gc, &groups)
		}
	}

	id := &Identity{
		Email:       claims.Email,
		DisplayName: claims.Name,
		AvatarURL:   claims.Picture,
		Groups:      groups,
		Source:      AuthSourceOIDC,
	}
	id.IsAdmin = isAdmin(h.cfg, id)

	if h.users != nil {
		if err := h.users.UpsertUser(r.Context(), id.Email, id.DisplayName, id.AvatarURL); err != nil {
			slog.WarnContext(r.Context(), "oidc: user upsert failed", "email", id.Email, "error", err)
		}
	}

	if err := issueSessionCookie(w, h.cfg, id); err != nil {
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}

	// Extract the post-login destination from the state. The value was
	// validated by HandleLogin, but re-check it so the callback never trusts
	// the cookie contents on its own.
	dest := "/"
	if parts := strings.SplitN(stateCookie.Value, "|", 2); len(parts) == 2 {
		dest = safeRedirectPath(parts[1])
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// safeRedirectPath returns rd if it is a safe same-site destination for a
// post-login redirect, or "/" otherwise. Safe means an absolute path
// ("/..."): no scheme, no host, not protocol-relative ("//host"), and no
// backslash — browsers normalise "\" to "/" in URLs, so "/\evil.com" would
// otherwise be treated as "//evil.com" and become an open redirect.
func safeRedirectPath(rd string) string {
	if rd == "" || rd[0] != '/' || strings.HasPrefix(rd, "//") || strings.ContainsAny(rd, "\\") {
		return "/"
	}
	u, err := url.Parse(rd)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" {
		return "/"
	}
	return rd
}

// HandleLogout clears the session cookie and redirects to home.
func (h *OIDCHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// OIDCMiddleware reads the session JWT cookie and populates the identity
// context when the cookie is valid. No-op when OIDC is disabled.
func OIDCMiddleware(cfg *config.Config, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.OIDC.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			if FromContext(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			id, issuedAt := parseSessionCookie(r, cfg)
			if id == nil {
				next.ServeHTTP(w, r)
				return
			}
			// Silently renew the session cookie once per renewal_delay interval.
			// This keeps active sessions alive without an OIDC round-trip. The
			// Set-Cookie header is added before the body is written, so it
			// works for both browser and Chrome extension requests.
			if !issuedAt.IsZero() && time.Since(issuedAt) > cfg.Session.RenewalDelay.Duration {
				if err := issueSessionCookie(w, cfg, id); err != nil {
					logger.WarnContext(r.Context(), "oidc: session renewal failed", "error", err)
				}
			}
			logger.DebugContext(r.Context(), "oidc: identity established from session cookie",
				"email", id.Email,
				"is_admin", id.IsAdmin)
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

// --- PKCE + cookie helpers -------------------------------------------------

func pkce() (verifier, challenge string, err error) {
	verifier, err = randomB64(32)
	if err != nil {
		return
	}
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

func randomB64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func setOIDCCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/auth/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func clearOIDCCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Path:     "/auth/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
