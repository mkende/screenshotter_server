package auth

import (
	"mime"
	"net/http"
)

// headerDecoder decodes RFC 2047 encoded-words. mime.WordDecoder handles the
// UTF-8, US-ASCII and ISO-8859-1 charsets on its own, which covers what
// identity headers carry in practice.
var headerDecoder = new(mime.WordDecoder)

// identityHeader returns the value of an identity header with any RFC 2047
// encoded-words decoded. HTTP header values are ASCII in practice, so
// proxies that pass non-ASCII identity data encode it this way: `tailscale
// serve` Q-encodes its Tailscale-User-* headers (e.g.
// "=?utf-8?q?Ren=C3=A9?=") whenever they hold a non-ASCII character. A value
// that is not encoded, or fails to decode, is returned unchanged.
func identityHeader(r *http.Request, name string) string {
	raw := r.Header.Get(name)
	if raw == "" {
		return ""
	}
	decoded, err := headerDecoder.DecodeHeader(raw)
	if err != nil {
		return raw
	}
	return decoded
}
