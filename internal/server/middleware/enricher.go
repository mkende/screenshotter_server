package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/mkende/screenshotter/server/internal/auth"
)

type contextLoggerKey struct{}

// WithContextLogger returns a context carrying the given logger.
func WithContextLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextLoggerKey{}, l)
}

// LoggerFromContext returns the request-scoped logger stored in ctx, or
// fallback when none is present.
func LoggerFromContext(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if l, ok := ctx.Value(contextLoggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return fallback
}

// LogEnricher returns a middleware that must run after all authentication
// middlewares. It:
//
//  1. Reads the identity (if any) from context to determine the auth source.
//  2. Uses r.Host as the "domain" attribute.
//  3. Fills the RequestAttrs (allocated by RequestLogger) so the deferred
//     request log line includes auth_source and domain.
//  4. Creates a child logger pre-loaded with those attributes and stores it
//     in context for handler use.
func LogEnricher(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authSource := ""
			if id := auth.FromContext(r.Context()); id != nil {
				authSource = string(id.Source)
			}
			domain := r.Host

			if a := RequestAttrsFromContext(r.Context()); a != nil {
				a.AuthSource = authSource
				a.Domain = domain
			}

			child := logger
			if authSource != "" {
				child = child.With("auth_source", authSource)
			}
			if domain != "" {
				child = child.With("domain", domain)
			}
			r = r.WithContext(WithContextLogger(r.Context(), child))

			next.ServeHTTP(w, r)
		})
	}
}
