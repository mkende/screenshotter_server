package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkende/screenshotter_server/internal/config"
	mw "github.com/mkende/screenshotter_server/internal/server/middleware"
)

const (
	chromeID      = "nnipkgjcfgekggpkclhdghbbfnpokdlg"
	firefoxOrigin = "moz-extension://0f6d1c2e-7b3a-4e59-9c1d-2a8b4f6e3d70"
)

func corsConfig(allowFirefox bool) *config.Config {
	return &config.Config{
		CanonicalAddress: "https://shots.example.com",
		CORS: config.CORSConfig{
			ExtensionIDs:           []string{chromeID},
			AllowFirefoxExtensions: allowFirefox,
		},
	}
}

func TestExtensionOriginMatcher(t *testing.T) {
	tests := []struct {
		origin       string
		allowFirefox bool
		want         bool
	}{
		{"chrome-extension://" + chromeID, false, true},
		{"chrome-extension://" + chromeID, true, true},
		{"chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true, false},
		// Firefox extensions only when allowed.
		{firefoxOrigin, false, false},
		{firefoxOrigin, true, true},
		// Nothing that merely resembles a Firefox extension origin.
		{"moz-extension://0F6D1C2E-7B3A-4E59-9C1D-2A8B4F6E3D70", true, false},
		{"moz-extension://not-a-uuid", true, false},
		{firefoxOrigin + ":8080", true, false},
		{firefoxOrigin + "/", true, false},
		{"https://" + firefoxOrigin[len("moz-extension://"):], true, false},
		{"https://evil.example.com", true, false},
		{"https://shots.example.com", true, false}, // Same origin is not an extension.
		{"", true, false},
		{"null", true, false},
	}

	for _, tc := range tests {
		isExtension := mw.ExtensionOriginMatcher(corsConfig(tc.allowFirefox))
		if got := isExtension(tc.origin); got != tc.want {
			t.Errorf("origin %q, allow_firefox_extensions=%v: got %v, want %v",
				tc.origin, tc.allowFirefox, got, tc.want)
		}
	}
}

func TestRequireSameOriginOrExtension(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name         string
		origin       string
		allowFirefox bool
		want         int
	}{
		{"canonical", "https://shots.example.com", false, http.StatusOK},
		{"chrome extension", "chrome-extension://" + chromeID, false, http.StatusOK},
		{"firefox extension allowed", firefoxOrigin, true, http.StatusOK},
		{"firefox extension not allowed", firefoxOrigin, false, http.StatusForbidden},
		{"other site", "https://evil.example.com", true, http.StatusForbidden},
		{"no origin", "", true, http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := mw.RequireSameOriginOrExtension(corsConfig(tc.allowFirefox))(ok)
			req := httptest.NewRequest(http.MethodPost, "https://shots.example.com/upload", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("got %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
