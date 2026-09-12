package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkende/screenshotter_server/internal/config"
)

// runTailscaleMiddleware sets up the middleware with the given trusted CIDRs,
// drives a request through it, and returns the Identity stored in the context
// (or nil).
func runTailscaleMiddleware(t *testing.T, cidrs []string, remoteAddr string, headers map[string]string) *Identity {
	t.Helper()
	cfg := &config.Config{TrustedProxy: cidrs}
	cfg.Tailscale.Enabled = true

	mw := TailscaleMiddleware(cfg, nil)

	var captured *Identity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = FromContext(r.Context())
	})
	h := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return captured
}

func TestTailscale_HeadersPresent_ReturnsIdentity(t *testing.T) {
	id := runTailscaleMiddleware(t, nil, "127.0.0.1:1234", map[string]string{
		"Tailscale-User-Login": "alice@example.com",
		"Tailscale-User-Name":  "Alice",
	})
	if id == nil {
		t.Fatal("expected identity, got nil")
	}
	if id.Email != "alice@example.com" {
		t.Errorf("Email: got %q, want %q", id.Email, "alice@example.com")
	}
	if id.DisplayName != "Alice" {
		t.Errorf("DisplayName: got %q, want %q", id.DisplayName, "Alice")
	}
	if id.Source != AuthSourceTailscale {
		t.Errorf("Source: got %q, want %q", id.Source, AuthSourceTailscale)
	}
}

func TestTailscale_MissingLoginHeader_ReturnsNil(t *testing.T) {
	id := runTailscaleMiddleware(t, nil, "127.0.0.1:1234", map[string]string{
		"Tailscale-User-Name": "Alice",
	})
	if id != nil {
		t.Error("expected nil identity when login header is absent")
	}
}

func TestTailscale_NoNameFallsBackToLogin(t *testing.T) {
	id := runTailscaleMiddleware(t, nil, "10.0.0.1:5678", map[string]string{
		"Tailscale-User-Login": "bob@example.com",
	})
	if id == nil {
		t.Fatal("expected identity, got nil")
	}
	if id.DisplayName != "bob@example.com" {
		t.Errorf("DisplayName should fall back to login, got %q", id.DisplayName)
	}
}

func TestTailscale_IPv4_AllowedIP_PassesThrough(t *testing.T) {
	id := runTailscaleMiddleware(t, []string{"10.0.0.0/8"}, "10.1.2.3:9999", map[string]string{
		"Tailscale-User-Login": "user@example.com",
	})
	if id == nil {
		t.Error("expected identity for allowed IPv4, got nil")
	}
}

func TestTailscale_IPv4_BlockedIP_ReturnsNil(t *testing.T) {
	id := runTailscaleMiddleware(t, []string{"10.0.0.0/8"}, "192.168.1.1:9999", map[string]string{
		"Tailscale-User-Login": "user@example.com",
	})
	if id != nil {
		t.Error("expected nil for blocked IPv4, got identity")
	}
}

func TestTailscale_IPv6_AllowedIP_PassesThrough(t *testing.T) {
	id := runTailscaleMiddleware(t, []string{"fd7a:115c:a1e0::/48"}, "[fd7a:115c:a1e0::1]:1234", map[string]string{
		"Tailscale-User-Login": "user@example.com",
	})
	if id == nil {
		t.Error("expected identity for allowed IPv6, got nil")
	}
}

func TestTailscale_IPv6_BlockedIP_ReturnsNil(t *testing.T) {
	id := runTailscaleMiddleware(t, []string{"fd7a:115c:a1e0::/48"}, "[2001:db8::1]:1234", map[string]string{
		"Tailscale-User-Login": "user@example.com",
	})
	if id != nil {
		t.Error("expected nil for blocked IPv6, got identity")
	}
}

func TestTailscale_EmptyAllowlist_TrustsAll(t *testing.T) {
	id := runTailscaleMiddleware(t, []string{}, "1.2.3.4:80", map[string]string{
		"Tailscale-User-Login": "anyone@example.com",
	})
	if id == nil {
		t.Error("expected identity when no CIDR restriction, got nil")
	}
}

func TestTailscale_Disabled_NoOp(t *testing.T) {
	cfg := &config.Config{}
	cfg.Tailscale.Enabled = false
	mw := TailscaleMiddleware(cfg, nil)

	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) != nil {
			t.Error("identity unexpectedly populated when tailscale disabled")
		}
		called = true
	})
	h := mw(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Tailscale-User-Login", "user@example.com")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Error("next handler not invoked")
	}
}
