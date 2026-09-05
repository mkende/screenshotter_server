package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/mkende/screenshotter/server/internal/config"
	mw "github.com/mkende/screenshotter/server/internal/server/middleware"
)

// serveWithSecurityHeaders runs one request through SecurityHeaders and
// returns the response headers and the nonce the handler saw in its context.
func serveWithSecurityHeaders(t *testing.T, cfg *config.Config) (http.Header, string) {
	t.Helper()
	var nonce string
	h := mw.SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce = mw.NonceFromContext(r.Context())
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	return rr.Header(), nonce
}

// cspDirective returns the value of the named directive in a CSP header.
func cspDirective(csp, name string) string {
	for _, d := range strings.Split(csp, ";") {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, name+" ") {
			return strings.TrimPrefix(d, name+" ")
		}
	}
	return ""
}

func TestSecurityHeaders_CSP(t *testing.T) {
	headers, nonce := serveWithSecurityHeaders(t, &config.Config{CanonicalAddress: "https://shots.example.com"})
	csp := headers.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header missing")
	}

	if nonce == "" {
		t.Fatal("no nonce stored in request context")
	}
	if got := cspDirective(csp, "script-src"); got != "'self' 'nonce-"+nonce+"'" {
		t.Errorf("script-src = %q, want 'self' plus the context nonce", got)
	}
	if strings.Contains(cspDirective(csp, "script-src"), "'unsafe-inline'") {
		t.Error("script-src must not allow 'unsafe-inline'")
	}

	// Avatars are hosted on the identity provider, so any https: image origin
	// must be allowed while everything else stays same-origin.
	if got := cspDirective(csp, "img-src"); got != "'self' data: blob: https:" {
		t.Errorf("img-src = %q, want 'self' data: blob: https:", got)
	}
	for _, d := range []string{"default-src", "connect-src", "form-action", "base-uri"} {
		if got := cspDirective(csp, d); got != "'self'" {
			t.Errorf("%s = %q, want 'self'", d, got)
		}
	}
	if got := cspDirective(csp, "object-src"); got != "'none'" {
		t.Errorf("object-src = %q, want 'none'", got)
	}
	if got := cspDirective(csp, "frame-ancestors"); got != "'none'" {
		t.Errorf("frame-ancestors = %q, want 'none'", got)
	}
}

func TestSecurityHeaders_NonceIsFreshPerRequest(t *testing.T) {
	cfg := &config.Config{}
	_, n1 := serveWithSecurityHeaders(t, cfg)
	_, n2 := serveWithSecurityHeaders(t, cfg)
	if n1 == n2 {
		t.Errorf("nonce reused across requests: %q", n1)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9+/]{22}==$`).MatchString(n1) {
		t.Errorf("nonce %q is not 16 random bytes in base64", n1)
	}
}

func TestSecurityHeaders_HSTSOnlyForHTTPSCanonical(t *testing.T) {
	headers, _ := serveWithSecurityHeaders(t, &config.Config{CanonicalAddress: "https://shots.example.com"})
	if got := headers.Get("Strict-Transport-Security"); got == "" {
		t.Error("expected Strict-Transport-Security for an https canonical address")
	}
	headers, _ = serveWithSecurityHeaders(t, &config.Config{CanonicalAddress: "http://localhost:8080"})
	if got := headers.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("unexpected Strict-Transport-Security %q for an http canonical address", got)
	}
	for k, want := range map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := headers.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}
