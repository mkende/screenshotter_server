package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkende/screenshotter_server/internal/config"
)

func newTestCfg(secret string, ttl time.Duration) *config.Config {
	return &config.Config{
		JWTSecret: secret,
		Session:   config.SessionConfig{TTL: config.NewTOMLDuration(ttl)},
	}
}

func TestJWT_RoundTrip(t *testing.T) {
	cfg := newTestCfg("super-secret-key-with-at-least-32-chars!", time.Hour)
	id := &Identity{Email: "alice@example.com", DisplayName: "Alice", AvatarURL: "https://example.com/a.png"}

	rr := httptest.NewRecorder()
	if err := issueSessionCookie(rr, cfg, id); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		req.AddCookie(c)
	}

	got, issuedAt := parseSessionCookie(req, cfg)
	if got == nil {
		t.Fatal("parseSessionCookie returned nil for valid cookie")
	}
	if got.Email != id.Email {
		t.Errorf("Email: got %q, want %q", got.Email, id.Email)
	}
	if got.DisplayName != id.DisplayName {
		t.Errorf("DisplayName: got %q, want %q", got.DisplayName, id.DisplayName)
	}
	if got.AvatarURL != id.AvatarURL {
		t.Errorf("AvatarURL: got %q, want %q", got.AvatarURL, id.AvatarURL)
	}
	if got.Source != AuthSourceOIDC {
		t.Errorf("Source: got %q, want %q", got.Source, AuthSourceOIDC)
	}
	if issuedAt.IsZero() {
		t.Error("expected non-zero issuedAt")
	}
	if d := time.Since(issuedAt); d < 0 || d > 5*time.Second {
		t.Errorf("issuedAt %v is not close to now", issuedAt)
	}
}

func TestJWT_ExpiredTokenReturnsNil(t *testing.T) {
	cfg := newTestCfg("super-secret-key-with-at-least-32-chars!", -time.Hour)
	id := &Identity{Email: "bob@example.com", DisplayName: "Bob"}

	rr := httptest.NewRecorder()
	if err := issueSessionCookie(rr, cfg, id); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		req.AddCookie(c)
	}

	if got, _ := parseSessionCookie(req, cfg); got != nil {
		t.Error("expected nil identity for expired token")
	}
}

func TestJWT_TamperedSignatureReturnsNil(t *testing.T) {
	cfg := newTestCfg("super-secret-key-with-at-least-32-chars!", time.Hour)
	id := &Identity{Email: "eve@example.com", DisplayName: "Eve"}

	rr := httptest.NewRecorder()
	if err := issueSessionCookie(rr, cfg, id); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}

	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookies set")
	}
	tampered := cookies[0].Value[:len(cookies[0].Value)-4] + "XXXX"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tampered})

	if got, _ := parseSessionCookie(req, cfg); got != nil {
		t.Error("expected nil identity for tampered token")
	}
}

func TestJWT_NoCookieReturnsNil(t *testing.T) {
	cfg := newTestCfg("super-secret-key-with-at-least-32-chars!", time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got, _ := parseSessionCookie(req, cfg); got != nil {
		t.Error("expected nil identity when no cookie present")
	}
}

func TestJWT_WrongSecretReturnsNil(t *testing.T) {
	cfgA := newTestCfg("super-secret-key-with-at-least-32-chars!", time.Hour)
	cfgB := newTestCfg("a-completely-different-secret-key-!!!!!!", time.Hour)
	id := &Identity{Email: "attacker@evil.com", DisplayName: "Hacker"}

	rr := httptest.NewRecorder()
	if err := issueSessionCookie(rr, cfgB, id); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		req.AddCookie(c)
	}
	if got, _ := parseSessionCookie(req, cfgA); got != nil {
		t.Error("expected nil identity when token signed with wrong key")
	}
}

func TestJWT_CookieName(t *testing.T) {
	cfg := newTestCfg("super-secret-key-with-at-least-32-chars!", time.Hour)
	id := &Identity{Email: "u@u.com", DisplayName: "U"}
	rr := httptest.NewRecorder()
	if err := issueSessionCookie(rr, cfg, id); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			found = true
		}
	}
	if !found {
		t.Errorf("cookie named %q not found in response", sessionCookieName)
	}
}

// craftStaleJWT builds a valid but old JWT for renewal tests without going
// through issueSessionCookie (which always uses time.Now as IssuedAt).
func craftStaleJWT(t *testing.T, secret string, issuedAgo, ttl time.Duration) string {
	t.Helper()
	now := time.Now()
	claims := sessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now.Add(-issuedAgo)),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl - issuedAgo)),
		},
		Email:       "alice@example.com",
		DisplayName: "Alice",
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("craftStaleJWT: %v", err)
	}
	return tok
}

func TestOIDCMiddleware_SilentRenewalAfterRenewalDelay(t *testing.T) {
	const secret = "super-secret-key-with-at-least-32-chars!"
	ttl := 24 * time.Hour
	renewalDelay := 2 * time.Hour
	cfg := &config.Config{
		JWTSecret: secret,
		Session: config.SessionConfig{
			TTL:          config.NewTOMLDuration(ttl),
			RenewalDelay: config.NewTOMLDuration(renewalDelay),
		},
		OIDC: config.OIDCConfig{Enabled: true},
	}

	// Token is 3h old — past the 2h renewal_delay threshold.
	tokenStr := craftStaleJWT(t, secret, 3*time.Hour, ttl)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tokenStr})
	rr := httptest.NewRecorder()

	var identityInCtx *Identity
	handler := OIDCMiddleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identityInCtx = FromContext(r.Context())
	}))
	handler.ServeHTTP(rr, req)

	if identityInCtx == nil || identityInCtx.Email != "alice@example.com" {
		t.Error("expected identity in context")
	}
	renewed := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			renewed = true
		}
	}
	if !renewed {
		t.Error("expected session cookie to be renewed for a token past half its TTL")
	}
}

func TestOIDCMiddleware_NoRenewalForFreshToken(t *testing.T) {
	const secret = "super-secret-key-with-at-least-32-chars!"
	ttl := 24 * time.Hour
	renewalDelay := 2 * time.Hour
	cfg := &config.Config{
		JWTSecret: secret,
		Session: config.SessionConfig{
			TTL:          config.NewTOMLDuration(ttl),
			RenewalDelay: config.NewTOMLDuration(renewalDelay),
		},
		OIDC: config.OIDCConfig{Enabled: true},
	}

	// Token is only 1h old — within the 2h renewal_delay threshold.
	tokenStr := craftStaleJWT(t, secret, time.Hour, ttl)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tokenStr})
	rr := httptest.NewRecorder()

	handler := OIDCMiddleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	handler.ServeHTTP(rr, req)

	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			t.Error("expected no Set-Cookie for a fresh token")
		}
	}
}
