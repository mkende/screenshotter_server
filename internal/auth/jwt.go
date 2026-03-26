package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const cookieName = "session"

// Claims are the custom JWT claims stored in the session cookie.
type Claims struct {
	UserID      string `json:"uid"`
	DisplayName string `json:"name"`
	Email       string `json:"email"`
	jwt.RegisteredClaims
}

// issueSessionCookie signs a JWT for the user and sets it as an HttpOnly cookie.
func (s *Service) issueSessionCookie(w http.ResponseWriter, userID, displayName, email string) error {
	now := time.Now()
	claims := Claims{
		UserID:      userID,
		DisplayName: displayName,
		Email:       email,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.Session.TTL.Duration)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.signingKey)
	if err != nil {
		return fmt.Errorf("sign jwt: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   int(s.cfg.Session.TTL.Duration.Seconds()),
	})
	return nil
}

// clearSessionCookie removes the session cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   -1,
	})
}

// parseSessionCookie validates the session cookie and returns its claims.
// Returns nil if the cookie is absent or invalid.
func (s *Service) parseSessionCookie(r *http.Request) *Claims {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	token, err := jwt.ParseWithClaims(cookie.Value, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.signingKey, nil
	})
	if err != nil || !token.Valid {
		return nil
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil
	}
	return claims
}
