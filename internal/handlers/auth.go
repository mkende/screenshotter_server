package handlers

import (
	"net/http"
)

// LoginHandler redirects to the OIDC provider (or 404 for Tailscale mode).
func (h *Handlers) LoginHandler(w http.ResponseWriter, r *http.Request) {
	h.auth.LoginHandler(w, r)
}

// CallbackHandler handles the OIDC redirect callback.
func (h *Handlers) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	h.auth.CallbackHandler(h.db)(w, r)
}

// LogoutHandler clears the session cookie.
func (h *Handlers) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	h.auth.LogoutHandler(w, r)
}
