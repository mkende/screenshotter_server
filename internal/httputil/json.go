// Package httputil holds small HTTP response helpers shared by the auth,
// middleware, and handlers packages.
package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "err", err)
	}
}

// WriteJSONError writes a JSON {"error": msg} response with the given status.
func WriteJSONError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}
