package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mkende/screenshotter/server/internal/config"
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

	got := parseSessionCookie(req, cfg)
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

	if got := parseSessionCookie(req, cfg); got != nil {
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

	if got := parseSessionCookie(req, cfg); got != nil {
		t.Error("expected nil identity for tampered token")
	}
}

func TestJWT_NoCookieReturnsNil(t *testing.T) {
	cfg := newTestCfg("super-secret-key-with-at-least-32-chars!", time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := parseSessionCookie(req, cfg); got != nil {
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
	if got := parseSessionCookie(req, cfgA); got != nil {
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
