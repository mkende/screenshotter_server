package auth

import (
	"net"
	"testing"

	"github.com/mkende/screenshotter_server/internal/config"
)

func TestTrustedProxyNets(t *testing.T) {
	nets := TrustedProxyNets(&config.Config{TrustedProxy: []string{"10.0.0.0/8", "fd00::/8"}})
	if len(nets) != 2 {
		t.Fatalf("got %d nets, want 2", len(nets))
	}
	if !IPInRanges(net.ParseIP("10.1.2.3"), nets) || !IPInRanges(net.ParseIP("fd00::1"), nets) {
		t.Error("expected addresses inside the configured ranges to match")
	}
	if IPInRanges(net.ParseIP("192.168.1.1"), nets) {
		t.Error("expected address outside the configured ranges not to match")
	}

	if got := TrustedProxyNets(&config.Config{}); len(got) != 0 {
		t.Errorf("empty trusted_proxy: got %v, want no nets", got)
	}

	defer func() {
		if recover() == nil {
			t.Error("expected panic for an invalid CIDR (config validation rejects these earlier)")
		}
	}()
	TrustedProxyNets(&config.Config{TrustedProxy: []string{"not-a-cidr"}})
}
