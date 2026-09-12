package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkende/screenshotter_server/internal/config"
	mw "github.com/mkende/screenshotter_server/internal/server/middleware"
)

// serveWithDomainRedirect runs one request through DomainRedirect and reports
// the response together with whether the wrapped handler was reached.
func serveWithDomainRedirect(t *testing.T, cfg *config.Config, r *http.Request) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	served := false
	h := mw.DomainRedirect(cfg)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		served = true
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr, served
}

// nonCanonicalRequest builds a plain-HTTP request to an internal host, which
// is never the canonical address used by these tests.
func nonCanonicalRequest(target string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Host = "10.0.0.5:8080"
	return r
}

func TestDomainRedirect_RedirectsToCanonical(t *testing.T) {
	cfg := &config.Config{CanonicalAddress: "https://shots.example.com"}
	rr, served := serveWithDomainRedirect(t, cfg, nonCanonicalRequest("/abcd1234?foo=bar"))
	if served {
		t.Error("handler ran, want a redirect instead")
	}
	if rr.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMovedPermanently)
	}
	if got := rr.Header().Get("Location"); got != "https://shots.example.com/abcd1234?foo=bar" {
		t.Errorf("Location = %q, want the canonical URL with path and query preserved", got)
	}
}

func TestDomainRedirect_NoRedirectParamServesInPlace(t *testing.T) {
	cfg := &config.Config{CanonicalAddress: "https://shots.example.com"}
	for _, target := range []string{"/abcd1234.png?no_redirect=1", "/abcd1234?foo=bar&no_redirect=1"} {
		rr, served := serveWithDomainRedirect(t, cfg, nonCanonicalRequest(target))
		if !served {
			t.Errorf("%s: handler did not run (status %d, Location %q)",
				target, rr.Code, rr.Header().Get("Location"))
		}
	}
}

func TestDomainRedirect_NoRedirectOtherValuesStillRedirect(t *testing.T) {
	cfg := &config.Config{CanonicalAddress: "https://shots.example.com"}
	for _, target := range []string{"/abcd1234?no_redirect=0", "/abcd1234?no_redirect=", "/abcd1234?no_redirect=true"} {
		rr, served := serveWithDomainRedirect(t, cfg, nonCanonicalRequest(target))
		if served {
			t.Errorf("%s: handler ran, want a redirect (only no_redirect=1 opts out)", target)
		}
		if rr.Code != http.StatusMovedPermanently {
			t.Errorf("%s: status = %d, want %d", target, rr.Code, http.StatusMovedPermanently)
		}
	}
}

func TestDomainRedirect_NoRedirectAppliesToPost(t *testing.T) {
	cfg := &config.Config{CanonicalAddress: "https://shots.example.com"}
	r := httptest.NewRequest(http.MethodPost, "/upload?no_redirect=1", nil)
	r.Host = "10.0.0.5:8080"
	if _, served := serveWithDomainRedirect(t, cfg, r); !served {
		t.Error("handler did not run for a POST carrying no_redirect=1")
	}
}

func TestDomainRedirect_NoCanonicalAddress(t *testing.T) {
	if _, served := serveWithDomainRedirect(t, &config.Config{}, nonCanonicalRequest("/abcd1234")); !served {
		t.Error("handler did not run without a canonical address configured")
	}
}
