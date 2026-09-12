package auth

import (
	"log/slog"
	"net/http"

	"github.com/mkende/screenshotter_server/internal/config"
)

// anonymousEmail is the fixed email used for the shared anonymous identity.
const anonymousEmail = "anonymous@localhost"

// AnonymousMiddleware populates the identity context with a single shared
// anonymous user when cfg.Anonymous.Enabled is true and no prior auth
// middleware has already identified the user.
//
// Intended for local development, testing, or isolated private instances.
// Do not enable on a publicly reachable server.
func AnonymousMiddleware(cfg *config.Config, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Anonymous.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			if FromContext(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			id := &Identity{
				Email:       anonymousEmail,
				DisplayName: "Anonymous",
				IsAdmin:     cfg.Anonymous.IsAdmin,
				Source:      AuthSourceAnonymous,
			}
			logger.DebugContext(r.Context(), "anonymous: no prior auth; using anonymous identity",
				"is_admin", id.IsAdmin)
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}
