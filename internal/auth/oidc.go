package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/mkende/screenshotter/server/internal/config"
	"golang.org/x/oauth2"
)

// oidcService implements the OIDC authorization-code flow with PKCE.
type oidcService struct {
	provider *gooidc.Provider
	verifier *gooidc.IDTokenVerifier
	oauth2   oauth2.Config
}

func newOIDCService(ctx context.Context, cfg *config.Config) (*oidcService, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.Auth.OIDC.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	oauth2cfg := oauth2.Config{
		ClientID:     cfg.Auth.OIDC.ClientID,
		ClientSecret: cfg.Auth.OIDC.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.Server.Domain + "/auth/callback",
		Scopes:       cfg.Auth.OIDC.Scopes,
	}
	return &oidcService{
		provider: provider,
		verifier: provider.Verifier(&gooidc.Config{ClientID: cfg.Auth.OIDC.ClientID}),
		oauth2:   oauth2cfg,
	}, nil
}

// LoginHandler redirects the browser to the OIDC provider.
// It stores PKCE verifier + state in short-lived signed cookies.
func (s *Service) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if s.oidcSvc == nil {
		http.NotFound(w, r)
		return
	}
	state, err := randomBase64(16)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	verifier, challenge, err := pkce()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Store state and verifier in short-lived HttpOnly cookies (10 min).
	setOIDCCookie(w, "oidc_state", state, 600)
	setOIDCCookie(w, "oidc_verifier", verifier, 600)

	authURL := s.oidcSvc.oauth2.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// CallbackHandler exchanges the auth code for an ID token, upserts the user,
// and issues a session cookie.
func (s *Service) CallbackHandler(db userUpserter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.oidcSvc == nil {
			http.NotFound(w, r)
			return
		}
		// Validate state.
		stateCookie, err := r.Cookie("oidc_state")
		if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		verifierCookie, err := r.Cookie("oidc_verifier")
		if err != nil {
			http.Error(w, "missing verifier", http.StatusBadRequest)
			return
		}
		clearOIDCCookie(w, "oidc_state")
		clearOIDCCookie(w, "oidc_verifier")

		// Exchange code.
		token, err := s.oidcSvc.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"),
			oauth2.SetAuthURLParam("code_verifier", verifierCookie.Value))
		if err != nil {
			http.Error(w, "code exchange failed", http.StatusBadRequest)
			return
		}
		rawID, ok := token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "no id_token in response", http.StatusBadRequest)
			return
		}
		idToken, err := s.oidcSvc.verifier.Verify(r.Context(), rawID)
		if err != nil {
			http.Error(w, "invalid id_token", http.StatusBadRequest)
			return
		}
		var claims struct {
			Sub   string `json:"sub"`
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "failed to read claims", http.StatusInternalServerError)
			return
		}
		if err := db.UpsertUser(r.Context(), claims.Sub, claims.Name, claims.Email); err != nil {
			http.Error(w, "failed to save user", http.StatusInternalServerError)
			return
		}
		if err := s.issueSessionCookie(w, claims.Sub, claims.Name, claims.Email); err != nil {
			http.Error(w, "failed to issue session", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// LogoutHandler clears the session cookie and redirects to /.
func (s *Service) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// userUpserter is the subset of db.DB used by CallbackHandler.
type userUpserter interface {
	UpsertUser(ctx context.Context, id, displayName, email string) error
}

// --- PKCE helpers ---

func pkce() (verifier, challenge string, err error) {
	verifier, err = randomBase64(32)
	if err != nil {
		return
	}
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

func randomBase64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// --- OIDC flow cookie helpers ---

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


