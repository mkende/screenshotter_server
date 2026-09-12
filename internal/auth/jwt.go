package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkende/screenshotter_server/internal/config"
)

// sessionCookieName is the JWT session cookie name.
const sessionCookieName = "screenshotter_session"

// sessionClaims is the JWT payload stored in the session cookie. It mirrors
// Identity so the browser can be re-authenticated from the cookie alone.
type sessionClaims struct {
	jwt.RegisteredClaims
	Email       string   `json:"email"`
	DisplayName string   `json:"name"`
	AvatarURL   string   `json:"picture,omitempty"`
	Groups      []string `json:"groups,omitempty"`
}

// issueSessionCookie signs a JWT for id and sets it as an HttpOnly cookie.
// SameSite=None is required so the Chrome extension can include the cookie
// when POSTing to /upload from chrome-extension:// origins.
func issueSessionCookie(w http.ResponseWriter, cfg *config.Config, id *Identity) error {
	now := time.Now()
	ttl := cfg.Session.TTL.Duration
	claims := sessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Email:       id.Email,
		DisplayName: id.DisplayName,
		AvatarURL:   id.AvatarURL,
		Groups:      id.Groups,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return fmt.Errorf("sign jwt: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   int(ttl.Seconds()),
	})
	return nil
}

// clearSessionCookie removes the session cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   -1,
	})
}

// parseSessionCookie validates the session cookie and returns an Identity and
// the token's IssuedAt time. Returns nil and a zero time if the cookie is
// absent or invalid.
func parseSessionCookie(r *http.Request, cfg *config.Config) (*Identity, time.Time) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, time.Time{}
	}
	var claims sessionClaims
	token, err := jwt.ParseWithClaims(cookie.Value, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(cfg.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, time.Time{}
	}
	id := &Identity{
		Email:       claims.Email,
		DisplayName: claims.DisplayName,
		AvatarURL:   claims.AvatarURL,
		Groups:      claims.Groups,
		Source:      AuthSourceOIDC,
	}
	id.IsAdmin = isAdmin(cfg, id)
	var issuedAt time.Time
	if claims.IssuedAt != nil {
		issuedAt = claims.IssuedAt.Time
	}
	return id, issuedAt
}
