// Package middleware provides HTTP middleware for the screenshotter server.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/mkende/screenshotter_server/internal/auth"
)

// RequestAttrs is a mutable bag of per-request attributes populated by inner
// middleware (LogEnricher) after authentication has run. RequestLogger stores a
// pointer to it in the context so it can be read in the deferred log line even
// though the enriched request object is not visible from the outer closure.
type RequestAttrs struct {
	AuthSource string // empty when unauthenticated
	Domain     string // value of the Host header
}

type requestAttrsKey struct{}

func withRequestAttrs(ctx context.Context, a *RequestAttrs) context.Context {
	return context.WithValue(ctx, requestAttrsKey{}, a)
}

// RequestAttrsFromContext returns the per-request mutable attrs, or nil.
func RequestAttrsFromContext(ctx context.Context) *RequestAttrs {
	a, _ := ctx.Value(requestAttrsKey{}).(*RequestAttrs)
	return a
}

// RequestLogger returns a middleware that logs each request using structured
// logging. It allocates a RequestAttrs and stores it in context so that
// LogEnricher (which runs after authentication) can fill in the auth source and
// domain before the deferred log line fires.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attrs := &RequestAttrs{}
			r = r.WithContext(withRequestAttrs(r.Context(), attrs))

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			next.ServeHTTP(ww, r)

			args := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			}
			if peer := auth.OriginalRemoteAddr(r.Context()); peer != "" && peer != r.RemoteAddr {
				args = append(args, "peer_addr", peer)
			}
			if attrs.AuthSource != "" {
				args = append(args, "auth_source", attrs.AuthSource)
			}
			if attrs.Domain != "" {
				args = append(args, "domain", attrs.Domain)
			}
			if rid := middleware.GetReqID(r.Context()); rid != "" {
				args = append(args, "request_id", rid)
			}
			logger.Info("request", args...)
		})
	}
}
