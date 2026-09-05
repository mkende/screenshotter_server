package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkende/screenshotter/server/internal/config"
	"golang.org/x/oauth2"
)

func TestSafeRedirectPath(t *testing.T) {
	tests := []struct {
		rd   string
		want string
	}{
		{"", "/"},
		{"/", "/"},
		{"/abc123", "/abc123"},
		{"/abc123?x=1&y=2", "/abc123?x=1&y=2"},
		{"/auth/done", "/auth/done"},
		{"/a/b/c#frag", "/a/b/c#frag"},
		// Off-site and protocol-relative destinations.
		{"https://evil.com/", "/"},
		{"//evil.com/", "/"},
		{"evil.com", "/"},
		{"javascript:alert(1)", "/"},
		// Backslash variants: browsers normalise "\" to "/" so these would
		// become protocol-relative URLs pointing off-site.
		{`/\evil.com`, "/"},
		{`/\/evil.com`, "/"},
		{`/abc\evil.com`, "/"},
		{`\\evil.com`, "/"},
	}
	for _, tt := range tests {
		if got := safeRedirectPath(tt.rd); got != tt.want {
			t.Errorf("safeRedirectPath(%q) = %q, want %q", tt.rd, got, tt.want)
		}
	}
}

// newTestOIDCHandler builds an OIDCHandler without contacting a provider; enough
// for HandleLogin, which only needs the oauth2 endpoint to build the auth URL.
func newTestOIDCHandler() *OIDCHandler {
	cfg := &config.Config{CanonicalAddress: "https://shots.example.com"}
	cfg.OIDC.Enabled = true
	return &OIDCHandler{
		cfg: cfg,
		oauth2: oauth2.Config{
			ClientID:    "client",
			Endpoint:    oauth2.Endpoint{AuthURL: "https://idp.example.com/auth", TokenURL: "https://idp.example.com/token"},
			RedirectURL: cfg.CanonicalAddress + "/auth/callback",
		},
	}
}

// stateDestination returns the destination part of the oidc_state cookie set
// by HandleLogin.
func stateDestination(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rr.Result().Cookies() {
		if c.Name == oidcStateCookie {
			parts := strings.SplitN(c.Value, "|", 2)
			if len(parts) != 2 {
				t.Fatalf("state cookie %q has no destination part", c.Value)
			}
			return parts[1]
		}
	}
	t.Fatal("no oidc_state cookie set")
	return ""
}

func TestHandleLogin_RejectsUnsafeRedirectDestinations(t *testing.T) {
	h := newTestOIDCHandler()
	tests := []struct {
		name string
		rd   string
		want string
	}{
		{"safe path is kept", "/abc123", "/abc123"},
		{"missing rd defaults to root", "", "/"},
		{"backslash open redirect", `/\evil.com`, "/"},
		{"protocol-relative", "//evil.com", "/"},
		{"absolute URL", "https://evil.com/x", "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "/auth/login"
			if tt.rd != "" {
				target += "?rd=" + strings.NewReplacer(`\`, "%5C", "/", "%2F", ":", "%3A").Replace(tt.rd)
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rr := httptest.NewRecorder()
			h.HandleLogin(rr, req)

			if rr.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rr.Code)
			}
			if got := stateDestination(t, rr); got != tt.want {
				t.Errorf("state destination = %q, want %q", got, tt.want)
			}
		})
	}
}
