package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	mw "github.com/mkende/screenshotter/server/internal/server/middleware"
)

func TestRequireMutationHeader(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw.RequireMutationHeader(ok)

	tests := []struct {
		method string
		header string
		want   int
	}{
		// Safe methods pass through regardless of the header.
		{http.MethodGet, "", http.StatusOK},
		{http.MethodHead, "", http.StatusOK},
		{http.MethodOptions, "", http.StatusOK},
		// Mutating methods require the header.
		{http.MethodPost, mw.MutationHeader, http.StatusOK},
		{http.MethodPatch, mw.MutationHeader, http.StatusOK},
		{http.MethodDelete, mw.MutationHeader, http.StatusOK},
		{http.MethodPut, mw.MutationHeader, http.StatusOK},
		// Mutating methods without the header are rejected.
		{http.MethodPost, "", http.StatusForbidden},
		{http.MethodPatch, "", http.StatusForbidden},
		{http.MethodDelete, "", http.StatusForbidden},
		{http.MethodPut, "", http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.method+"_header="+tc.header, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", nil)
			if tc.header != "" {
				req.Header.Set(tc.header, "1")
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Errorf("got %d, want %d", rr.Code, tc.want)
			}
		})
	}
}
