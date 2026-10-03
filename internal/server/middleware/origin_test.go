package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkende/screenshotter_server/internal/config"
	mw "github.com/mkende/screenshotter_server/internal/server/middleware"
)

const (
	chromeOrigin  = "chrome-extension://nnipkgjcfgekggpkclhdghbbfnpokdlg"
	firefoxOrigin = "moz-extension://0f6d1c2e-7b3a-4e59-9c1d-2a8b4f6e3d70"
)

func TestIsExtensionOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{chromeOrigin, true},
		{"chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{firefoxOrigin, true},
		// Nothing that merely resembles an extension origin.
		{"chrome-extension://nnipkgjcfgekggpkclhdghbbfnpokdl", false},  // 31 letters
		{"chrome-extension://nnipkgjcfgekggpkclhdghbbfnpokdlz", false}, // not a–p
		{"chrome-extension://NNIPKGJCFGEKGGPKCLHDGHBBFNPOKDLG", false},
		{chromeOrigin + "/", false},
		{chromeOrigin + ":8080", false},
		{"moz-extension://0F6D1C2E-7B3A-4E59-9C1D-2A8B4F6E3D70", false},
		{"moz-extension://not-a-uuid", false},
		{firefoxOrigin + ":8080", false},
		{firefoxOrigin + "/", false},
		{"https://nnipkgjcfgekggpkclhdghbbfnpokdlg", false},
		{"https://evil.example.com", false},
		{"", false},
		{"null", false},
	}

	for _, tc := range tests {
		if got := mw.IsExtensionOrigin(tc.origin); got != tc.want {
			t.Errorf("IsExtensionOrigin(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}
}

func TestRequireSameOriginOrExtension(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw.RequireSameOriginOrExtension(&config.Config{
		CanonicalAddress: "https://shots.example.com",
	})(ok)

	tests := []struct {
		name   string
		origin string
		want   int
	}{
		{"canonical", "https://shots.example.com", http.StatusOK},
		{"chrome extension", chromeOrigin, http.StatusOK},
		{"firefox extension", firefoxOrigin, http.StatusOK},
		{"other site", "https://evil.example.com", http.StatusForbidden},
		{"no origin", "", http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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
