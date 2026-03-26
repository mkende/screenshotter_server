package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkende/screenshotter/server/internal/config"
)

// makeTailscaleService constructs a tailscaleService with the given proxy CIDRs.
func makeTailscaleService(t *testing.T, cidrs []string) *tailscaleService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Auth.Tailscale.ProxyIPs = cidrs
	svc, err := newTailscaleService(cfg)
	if err != nil {
		t.Fatalf("newTailscaleService: %v", err)
	}
	return svc
}

// requestWithHeaders returns a new request with the given remote addr and headers.
func requestWithHeaders(remoteAddr string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestTailscale_HeadersPresent_ReturnsClaims(t *testing.T) {
	svc := makeTailscaleService(t, nil) // no IP restriction
	req := requestWithHeaders("127.0.0.1:1234", map[string]string{
		"Tailscale-User-Login": "alice@example.com",
		"Tailscale-User-Name":  "Alice",
	})
	claims := svc.claimsFromHeaders(req)
	if claims == nil {
		t.Fatal("expected claims, got nil")
	}
	if claims.UserID != "alice@example.com" {
		t.Errorf("UserID: got %q, want %q", claims.UserID, "alice@example.com")
	}
	if claims.DisplayName != "Alice" {
		t.Errorf("DisplayName: got %q, want %q", claims.DisplayName, "Alice")
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("Email: got %q, want %q", claims.Email, "alice@example.com")
	}
}

func TestTailscale_MissingLoginHeader_ReturnsNil(t *testing.T) {
	svc := makeTailscaleService(t, nil)
	// Only name, no login header.
	req := requestWithHeaders("127.0.0.1:1234", map[string]string{
		"Tailscale-User-Name": "Alice",
	})
	if claims := svc.claimsFromHeaders(req); claims != nil {
		t.Error("expected nil claims when login header is absent")
	}
}

func TestTailscale_NoNameFallsBackToLogin(t *testing.T) {
	svc := makeTailscaleService(t, nil)
	req := requestWithHeaders("10.0.0.1:5678", map[string]string{
		"Tailscale-User-Login": "bob@example.com",
		// deliberately no Tailscale-User-Name header
	})
	claims := svc.claimsFromHeaders(req)
	if claims == nil {
		t.Fatal("expected claims, got nil")
	}
	if claims.DisplayName != "bob@example.com" {
		t.Errorf("DisplayName should fall back to login, got %q", claims.DisplayName)
	}
}

func TestTailscale_IPv4_AllowedIP_PassesThrough(t *testing.T) {
	svc := makeTailscaleService(t, []string{"10.0.0.0/8"})
	req := requestWithHeaders("10.1.2.3:9999", map[string]string{
		"Tailscale-User-Login": "user@example.com",
	})
	if claims := svc.claimsFromHeaders(req); claims == nil {
		t.Error("expected claims for allowed IPv4, got nil")
	}
}

func TestTailscale_IPv4_BlockedIP_ReturnsNil(t *testing.T) {
	svc := makeTailscaleService(t, []string{"10.0.0.0/8"})
	req := requestWithHeaders("192.168.1.1:9999", map[string]string{
		"Tailscale-User-Login": "user@example.com",
	})
	if claims := svc.claimsFromHeaders(req); claims != nil {
		t.Error("expected nil for blocked IPv4, got claims")
	}
}

func TestTailscale_IPv6_AllowedIP_PassesThrough(t *testing.T) {
	svc := makeTailscaleService(t, []string{"fd7a:115c:a1e0::/48"})
	req := requestWithHeaders("[fd7a:115c:a1e0::1]:1234", map[string]string{
		"Tailscale-User-Login": "user@example.com",
	})
	if claims := svc.claimsFromHeaders(req); claims == nil {
		t.Error("expected claims for allowed IPv6, got nil")
	}
}

func TestTailscale_IPv6_BlockedIP_ReturnsNil(t *testing.T) {
	svc := makeTailscaleService(t, []string{"fd7a:115c:a1e0::/48"})
	req := requestWithHeaders("[2001:db8::1]:1234", map[string]string{
		"Tailscale-User-Login": "user@example.com",
	})
	if claims := svc.claimsFromHeaders(req); claims != nil {
		t.Error("expected nil for blocked IPv6, got claims")
	}
}

func TestTailscale_EmptyAllowlist_TrustsAll(t *testing.T) {
	// No CIDRs → any source is trusted.
	svc := makeTailscaleService(t, []string{})
	req := requestWithHeaders("1.2.3.4:80", map[string]string{
		"Tailscale-User-Login": "anyone@example.com",
	})
	if claims := svc.claimsFromHeaders(req); claims == nil {
		t.Error("expected claims when no CIDR restriction, got nil")
	}
}
