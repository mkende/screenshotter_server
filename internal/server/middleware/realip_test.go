package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkende/screenshotter/server/internal/config"
	mw "github.com/mkende/screenshotter/server/internal/server/middleware"
)

// TestTrustedRealIP exercises the rightmost-not-trusted X-Forwarded-For
// selection and the trusted-peer gate.
func TestTrustedRealIP(t *testing.T) {
	cfg := &config.Config{TrustedProxy: []string{"10.0.0.0/8"}}

	tests := []struct {
		name       string
		remoteAddr string // TCP peer
		xff        string
		xRealIP    string
		want       string // expected r.RemoteAddr seen by the handler
	}{
		{
			name:       "untrusted peer ignores headers",
			remoteAddr: "1.2.3.4:5000",
			xff:        "8.8.8.8",
			want:       "1.2.3.4:5000",
		},
		{
			name:       "trusted peer single entry",
			remoteAddr: "10.0.0.1:5000",
			xff:        "1.2.3.4",
			want:       "1.2.3.4",
		},
		{
			name:       "forged prefix is ignored (rightmost untrusted wins)",
			remoteAddr: "10.0.0.1:5000",
			xff:        "9.9.9.9, 1.2.3.4",
			want:       "1.2.3.4",
		},
		{
			name:       "skips internal trusted hops",
			remoteAddr: "10.0.0.1:5000",
			xff:        "9.9.9.9, 1.2.3.4, 10.0.0.2",
			want:       "1.2.3.4",
		},
		{
			name:       "falls back to X-Real-IP when no XFF",
			remoteAddr: "10.0.0.1:5000",
			xRealIP:    "1.2.3.4",
			want:       "1.2.3.4",
		},
		{
			name:       "no forwarding headers keeps peer",
			remoteAddr: "10.0.0.1:5000",
			want:       "10.0.0.1:5000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			handler := mw.TrustedRealIP(cfg)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = r.RemoteAddr
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xRealIP != "" {
				req.Header.Set("X-Real-IP", tc.xRealIP)
			}

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if got != tc.want {
				t.Errorf("RemoteAddr = %q, want %q", got, tc.want)
			}
		})
	}
}
