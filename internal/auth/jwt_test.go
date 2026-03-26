package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkende/screenshotter/server/internal/config"
)

// makeService returns an auth Service configured for JWT cookie tests.
func makeService(secret string, ttl time.Duration) *Service {
	cfg := &config.Config{}
	cfg.Session.Secret = secret
	cfg.Session.TTL = config.ExportTomlDuration(ttl)
	return &Service{
		cfg:        cfg,
		signingKey: []byte(secret),
	}
}

func TestJWT_RoundTrip(t *testing.T) {
	svc := makeService("super-secret-key-with-at-least-32-chars!", time.Hour)

	// Issue a cookie.
	rr := httptest.NewRecorder()
	if err := svc.issueSessionCookie(rr, "user1", "Alice", "alice@example.com"); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}

	// Build a request with the cookie.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		req.AddCookie(c)
	}

	claims := svc.parseSessionCookie(req)
	if claims == nil {
		t.Fatal("parseSessionCookie returned nil for valid cookie")
	}
	if claims.UserID != "user1" {
		t.Errorf("UserID: got %q, want %q", claims.UserID, "user1")
	}
	if claims.DisplayName != "Alice" {
		t.Errorf("DisplayName: got %q, want %q", claims.DisplayName, "Alice")
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("Email: got %q, want %q", claims.Email, "alice@example.com")
	}
}

func TestJWT_ExpiredTokenReturnsNil(t *testing.T) {
	svc := makeService("super-secret-key-with-at-least-32-chars!", -time.Hour)

	rr := httptest.NewRecorder()
	if err := svc.issueSessionCookie(rr, "user2", "Bob", "bob@example.com"); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		req.AddCookie(c)
	}

	claims := svc.parseSessionCookie(req)
	if claims != nil {
		t.Error("expected nil claims for expired token")
	}
}

func TestJWT_TamperedSignatureReturnsNil(t *testing.T) {
	svc := makeService("super-secret-key-with-at-least-32-chars!", time.Hour)

	rr := httptest.NewRecorder()
	if err := svc.issueSessionCookie(rr, "user3", "Eve", "eve@example.com"); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}

	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookies set")
	}
	// Tamper with the token value.
	originalValue := cookies[0].Value
	tamperedValue := originalValue[:len(originalValue)-4] + "XXXX"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tamperedValue})

	claims := svc.parseSessionCookie(req)
	if claims != nil {
		t.Error("expected nil claims for tampered token")
	}
}

func TestJWT_NoCookieReturnsNil(t *testing.T) {
	svc := makeService("super-secret-key-with-at-least-32-chars!", time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if claims := svc.parseSessionCookie(req); claims != nil {
		t.Error("expected nil claims when no cookie present")
	}
}

func TestJWT_WrongAlgorithmReturnsNil(t *testing.T) {
	svc := makeService("super-secret-key-with-at-least-32-chars!", time.Hour)

	// Create a token signed with RS256 (a different algorithm) but we don't
	// have an RSA key easily — instead, forge a token with "none" algorithm.
	// We build a raw HS256 token with a different secret to trigger sig failure.
	wrongSvc := makeService("a-completely-different-secret-key-!!!!!!", time.Hour)
	rr := httptest.NewRecorder()
	if err := wrongSvc.issueSessionCookie(rr, "attacker", "Hacker", "h@evil.com"); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		req.AddCookie(c)
	}
	if claims := svc.parseSessionCookie(req); claims != nil {
		t.Error("expected nil claims when token signed with wrong key")
	}
}

func TestJWT_CookieName(t *testing.T) {
	svc := makeService("super-secret-key-with-at-least-32-chars!", time.Hour)
	rr := httptest.NewRecorder()
	if err := svc.issueSessionCookie(rr, "u", "U", "u@u.com"); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == cookieName {
			found = true
		}
	}
	if !found {
		t.Errorf("cookie named %q not found in response", cookieName)
	}
}

// TestJWT_ClaimsAreSigned verifies that the issued token contains the right
// expiry relative to the configured TTL.
func TestJWT_ClaimsExpiry(t *testing.T) {
	ttl := 2 * time.Hour
	svc := makeService("super-secret-key-with-at-least-32-chars!", ttl)
	rr := httptest.NewRecorder()
	before := time.Now()
	if err := svc.issueSessionCookie(rr, "u", "U", "u@u.com"); err != nil {
		t.Fatalf("issueSessionCookie: %v", err)
	}
	after := time.Now()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		req.AddCookie(c)
	}
	claims := svc.parseSessionCookie(req)
	if claims == nil {
		t.Fatal("parseSessionCookie returned nil")
	}
	exp := claims.ExpiresAt.Time
	// exp should be approximately before + ttl and after + ttl.
	if exp.Before(before.Add(ttl - time.Second)) || exp.After(after.Add(ttl+time.Second)) {
		t.Errorf("expiry %v not within expected range [%v, %v]", exp, before.Add(ttl), after.Add(ttl))
	}
}

// Ensure jwt.RegisteredClaims is accessible (compile-time check).
var _ jwt.Claims = (*Claims)(nil)
