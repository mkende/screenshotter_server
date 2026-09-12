package middleware

import (
	"net/http"

	"github.com/mkende/screenshotter_server/internal/httputil"
)

// MutationHeader is the custom request header that all state-changing requests
// must carry. Its presence forces a CORS preflight on cross-origin requests,
// which the server's CORS allowlist then gates, providing a framework-level
// CSRF defence that does not depend on per-endpoint Origin checks.
const MutationHeader = "X-Screenshotter-Request"

// RequireMutationHeader returns middleware that rejects state-changing requests
// (POST, PUT, PATCH, DELETE) that do not include the MutationHeader header.
//
// A browser form or a cross-origin fetch() without the header either gets
// blocked here (direct) or triggers a CORS preflight whose failure prevents
// the request from being sent at all. Safe methods (GET, HEAD, OPTIONS) are
// passed through unchanged.
func RequireMutationHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(MutationHeader) == "" {
			httputil.WriteJSONError(w, http.StatusForbidden, "missing "+MutationHeader+" header")
			return
		}
		next.ServeHTTP(w, r)
	})
}
