// Package config provides configuration loading and validation for the
// screenshotter server.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// AnonymousConfig holds settings for anonymous (user-less) authentication.
// In this mode every request is automatically treated as a single shared
// anonymous user. Intended for local development, testing, or private
// instances where no real user management is needed.
// WARNING: Do not enable on a publicly reachable server.
type AnonymousConfig struct {
	// Enabled controls whether anonymous auth is active.
	Enabled bool `toml:"enabled"`
	// IsAdmin grants the anonymous user full admin privileges when true.
	// Default: false.
	IsAdmin bool `toml:"is_admin"`
}

// TailscaleConfig holds settings for Tailscale header-based authentication.
// Trusted proxy IPs come from the top-level trusted_proxy setting.
type TailscaleConfig struct {
	// Enabled controls whether Tailscale header-based auth is active.
	Enabled bool `toml:"enabled"`
}

// ProxyAuthConfig holds settings for reverse-proxy header-based authentication.
// Header names default to the de-facto standard used by Authelia.
type ProxyAuthConfig struct {
	// Enabled controls whether proxy header-based auth is active.
	Enabled bool `toml:"enabled"`
	// UserHeader is the header containing the authenticated user's login name.
	// Used as a fallback identifier when EmailHeader is absent.
	// Defaults to "Remote-User".
	UserHeader string `toml:"user_header"`
	// EmailHeader is the header containing the user's email address. When
	// present it is used as the primary user identifier; UserHeader is then
	// treated as a supplementary username. Defaults to "Remote-Email".
	EmailHeader string `toml:"email_header"`
	// NameHeader is the header containing the user's display name.
	// Defaults to "Remote-Name".
	NameHeader string `toml:"name_header"`
	// GroupsHeader is the header containing a comma-separated list of the
	// user's group memberships. Defaults to "Remote-Groups".
	GroupsHeader string `toml:"groups_header"`
}

// OIDCConfig holds settings for OpenID Connect authentication.
type OIDCConfig struct {
	// Enabled controls whether OIDC auth is active.
	Enabled bool `toml:"enabled"`
	// Issuer is the OIDC provider issuer URL.
	Issuer string `toml:"issuer"`
	// ClientID is the OAuth2 client identifier.
	ClientID string `toml:"client_id"`
	// ClientSecret is the OAuth2 client secret. Mutually exclusive with
	// ClientSecretEnvVar; exactly one must be set when OIDC is enabled.
	ClientSecret string `toml:"client_secret"`
	// ClientSecretEnvVar is the name of an environment variable whose value is
	// used as the OAuth2 client secret. Mutually exclusive with ClientSecret.
	ClientSecretEnvVar string `toml:"client_secret_env_var"`
	// Scopes is the list of OAuth2 scopes to request.
	// Defaults to ["openid", "email", "profile"].
	Scopes []string `toml:"scopes"`
	// GroupsClaim is the JWT claim name that contains group memberships.
	// Defaults to "groups".
	GroupsClaim string `toml:"groups_claim"`
}

// DBConfig holds database connection settings.
type DBConfig struct {
	// Driver selects the database backend. Valid values: "sqlite", "postgres".
	Driver string `toml:"driver"`
	// DSN is the data source name / connection string.
	DSN string `toml:"dsn"`
}

// ServerConfig holds server-level filesystem/network settings specific to
// screenshotter (upload limits, storage path, etc.).
type ServerConfig struct {
	// StoragePath is the directory where uploaded images and thumbnails live.
	StoragePath string `toml:"storage_path"`
	// MaxUploadMB is the maximum accepted upload size in megabytes.
	MaxUploadMB int64 `toml:"max_upload_mb"`
	// AssetsPath is an optional directory of static files. When set,
	// /favicon.ico is served from <assets_path>/favicon.ico (unless
	// FaviconPath overrides it at the top level).
	AssetsPath string `toml:"assets_path"`
}

// SessionConfig controls session cookie lifetime.
type SessionConfig struct {
	// TTL is how long an issued session JWT remains valid.
	TTL TOMLDuration `toml:"ttl"`
}

// CORSConfig lists the Chrome extension IDs allowed to make credentialed
// cross-origin requests (required for extension uploads).
type CORSConfig struct {
	ExtensionIDs []string `toml:"extension_ids"`
}

// IDConfig controls the length of generated image IDs.
type IDConfig struct {
	Length int `toml:"length"`
}

// HomeConfig controls the home-page gallery layout.
type HomeConfig struct {
	// Cols is the maximum number of thumbnail columns (1–10). Default: 5.
	Cols int `toml:"cols"`
	// PageSize is the number of screenshots shown per page. Default: 20.
	PageSize int `toml:"page_size"`
}

// RateLimitConfig controls the built-in per-IP rate limiter applied to the
// unauthenticated image routes.
type RateLimitConfig struct {
	// RequestsPerSecond is the maximum number of requests allowed per IP per
	// second (default: 5). Set to 0 to disable.
	RequestsPerSecond int `toml:"requests_per_second"`
	// RequestsPerMinute is the maximum number of requests allowed per IP per
	// minute (default: 50). Set to 0 to disable.
	RequestsPerMinute int `toml:"requests_per_minute"`
}

// Config is the top-level application configuration.
type Config struct {
	// ListenAddr is the TCP address the HTTP server binds to.
	// Defaults to "0.0.0.0:8080".
	ListenAddr string `toml:"listen_addr"`

	// CanonicalAddress is the public base URL for this instance, including
	// scheme. Example: "https://shots.example.com".
	// Required when OIDC is enabled (used to build the callback URL).
	// When set, all non-exempt requests that arrive on a different scheme or
	// host are redirected here with a 301, preserving path and query.
	CanonicalAddress string `toml:"canonical_address"`

	// TrustedProxy is the list of IP ranges (CIDR notation) from which
	// proxy-forwarding headers are trusted: X-Forwarded-Proto for scheme
	// detection, X-Forwarded-For / X-Real-IP for client IP recovery, and
	// Tailscale-User-* / Remote-* for header-based authentication. Required
	// when tailscale or proxy_auth is enabled.
	TrustedProxy []string `toml:"trusted_proxy"`

	// Title is the human-readable name shown in the UI.
	// Defaults to "Screenshotter".
	Title string `toml:"title"`

	// FaviconPath is a filesystem path to a custom favicon file. When empty,
	// the server falls back to <server.assets_path>/favicon.ico.
	FaviconPath string `toml:"favicon_path"`

	// JWTSecret is the HMAC secret used to sign and verify session JWT cookies.
	// Use a long random string (>= 32 bytes). Mutually exclusive with
	// JWTSecretEnvVar.
	JWTSecret string `toml:"jwt_secret"`

	// JWTSecretEnvVar is the name of an environment variable whose value is
	// used as the JWT HMAC secret.
	JWTSecretEnvVar string `toml:"jwt_secret_env_var"`

	// LogLevel controls the minimum severity of log messages emitted by the
	// server. Supported values: "debug", "info", "warn", "error". Defaults
	// to "info".
	LogLevel string `toml:"log_level"`

	// AdminEmails lists the email addresses of users with admin privileges.
	AdminEmails []string `toml:"admin_emails"`

	// AdminGroups is a list of OIDC / proxy-auth group names whose members are
	// treated as admins. Requires groups_claim (or proxy groups header) to be
	// correctly configured.
	AdminGroups []string `toml:"admin_groups"`

	// Anonymous holds settings for anonymous (user-less) authentication.
	Anonymous AnonymousConfig `toml:"anonymous"`

	// Tailscale holds settings for Tailscale header-based authentication.
	Tailscale TailscaleConfig `toml:"tailscale"`

	// ProxyAuth holds settings for reverse-proxy header-based authentication.
	ProxyAuth ProxyAuthConfig `toml:"proxy_auth"`

	// OIDC holds settings for OpenID Connect authentication.
	OIDC OIDCConfig `toml:"oidc"`

	// DB holds database connection settings.
	DB DBConfig `toml:"db"`

	// Server holds screenshotter-specific server filesystem settings.
	Server ServerConfig `toml:"server"`

	// Session holds session cookie settings.
	Session SessionConfig `toml:"session"`

	// CORS holds cross-origin settings for the Chrome extension.
	CORS CORSConfig `toml:"cors"`

	// ID controls the length of generated image IDs.
	ID IDConfig `toml:"id"`

	// Home controls the home-page gallery layout.
	Home HomeConfig `toml:"home"`

	// RateLimit controls the per-IP rate limiter on the image routes.
	RateLimit RateLimitConfig `toml:"ratelimit"`
}

// TOMLDuration is a time.Duration that unmarshals from a TOML string like "720h".
type TOMLDuration struct{ time.Duration }

// NewTOMLDuration wraps d in a TOMLDuration. Useful in tests.
func NewTOMLDuration(d time.Duration) TOMLDuration { return TOMLDuration{d} }

// UnmarshalText parses a Go duration string.
func (d *TOMLDuration) UnmarshalText(b []byte) error {
	dur, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", b, err)
	}
	d.Duration = dur
	return nil
}

// CanonicalHost returns the host component of CanonicalAddress, or "".
func (c *Config) CanonicalHost() string {
	if c.CanonicalAddress == "" {
		return ""
	}
	u, err := url.Parse(c.CanonicalAddress)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// CanonicalScheme returns the scheme component of CanonicalAddress, or "".
func (c *Config) CanonicalScheme() string {
	if c.CanonicalAddress == "" {
		return ""
	}
	u, err := url.Parse(c.CanonicalAddress)
	if err != nil || u.Scheme == "" {
		return ""
	}
	return u.Scheme
}

// AnyAuthEnabled reports whether at least one auth provider is enabled.
func (c *Config) AnyAuthEnabled() bool {
	return c.Anonymous.Enabled || c.Tailscale.Enabled || c.ProxyAuth.Enabled || c.OIDC.Enabled
}

// Load parses the TOML config at path, applies defaults, resolves secrets, and
// validates the result.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}
	cfg := defaults()
	meta, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		return nil, fmt.Errorf("parsing config file %q: unknown configuration key(s): %v", path, unknown)
	}

	if err := resolveSecrets(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		ListenAddr:  "0.0.0.0:8080",
		Title:       "Screenshotter",
		LogLevel:    "info",
		Server:      ServerConfig{MaxUploadMB: 4},
		Session:     SessionConfig{TTL: TOMLDuration{720 * time.Hour}},
		ID:          IDConfig{Length: 8},
		Home:        HomeConfig{Cols: 5, PageSize: 20},
		RateLimit:   RateLimitConfig{RequestsPerSecond: 5, RequestsPerMinute: 50},
		ProxyAuth: ProxyAuthConfig{
			UserHeader:   "Remote-User",
			EmailHeader:  "Remote-Email",
			NameHeader:   "Remote-Name",
			GroupsHeader: "Remote-Groups",
		},
		OIDC: OIDCConfig{
			Scopes:      []string{"openid", "email", "profile"},
			GroupsClaim: "groups",
		},
		DB: DBConfig{Driver: "sqlite"},
	}
}

// resolveSecret reads a secret either directly or from an environment
// variable. Exactly one of the two forms may be set.
func resolveSecret(field, value, envField, envName string) (string, error) {
	if value != "" && envName != "" {
		return "", fmt.Errorf("cannot set both %s and %s", field, envField)
	}
	if envName == "" {
		return value, nil
	}
	resolved, ok := os.LookupEnv(envName)
	if !ok {
		return "", fmt.Errorf("%s: environment variable %q is not set", envField, envName)
	}
	return resolved, nil
}

func resolveSecrets(c *Config) error {
	var err error
	c.JWTSecret, err = resolveSecret("jwt_secret", c.JWTSecret, "jwt_secret_env_var", c.JWTSecretEnvVar)
	if err != nil {
		return err
	}
	c.OIDC.ClientSecret, err = resolveSecret("oidc.client_secret", c.OIDC.ClientSecret, "oidc.client_secret_env_var", c.OIDC.ClientSecretEnvVar)
	return err
}

func validate(c *Config) error {
	// canonical_address
	if c.CanonicalAddress != "" {
		u, err := url.Parse(c.CanonicalAddress)
		if err != nil || u.Host == "" || u.Scheme == "" {
			return fmt.Errorf("canonical_address must be a valid URL with scheme and host (got %q)", c.CanonicalAddress)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("canonical_address scheme must be http or https (got %q)", u.Scheme)
		}
		// Trim any trailing slash so callers can always concatenate a path.
		c.CanonicalAddress = strings.TrimRight(c.CanonicalAddress, "/")
	}

	// trusted_proxy CIDRs
	for _, cidr := range c.TrustedProxy {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("trusted_proxy: invalid CIDR %q: %w", cidr, err)
		}
	}

	// auth providers
	if !c.AnyAuthEnabled() {
		return errors.New("at least one authentication provider must be enabled (anonymous, tailscale, proxy_auth, or oidc)")
	}
	if c.Tailscale.Enabled && len(c.TrustedProxy) == 0 {
		return errors.New("trusted_proxy must be set when tailscale auth is enabled")
	}
	if c.ProxyAuth.Enabled && len(c.TrustedProxy) == 0 {
		return errors.New("trusted_proxy must be set when proxy_auth is enabled")
	}
	if c.OIDC.Enabled {
		if c.CanonicalAddress == "" {
			return errors.New("canonical_address is required when oidc is enabled")
		}
		if c.OIDC.Issuer == "" || c.OIDC.ClientID == "" || c.OIDC.ClientSecret == "" {
			return errors.New("oidc: issuer, client_id, and client_secret are all required when oidc is enabled")
		}
	}

	// jwt_secret: required whenever we issue JWT cookies. OIDC is the only
	// backend that does; other backends never set a session cookie.
	if c.OIDC.Enabled {
		if c.JWTSecret == "" {
			return errors.New("jwt_secret is required when oidc is enabled")
		}
		if len(c.JWTSecret) < 32 {
			return fmt.Errorf("jwt_secret must be at least 32 characters")
		}
	}

	// server / storage
	if c.Server.StoragePath == "" {
		return errors.New("server.storage_path is required")
	}
	if c.Server.MaxUploadMB <= 0 {
		return errors.New("server.max_upload_mb must be positive")
	}

	// db
	switch c.DB.Driver {
	case "sqlite", "postgres":
		// valid
	default:
		return fmt.Errorf("db.driver must be \"sqlite\" or \"postgres\", got %q", c.DB.Driver)
	}
	if c.DB.DSN == "" {
		return errors.New("db.dsn is required")
	}

	// id / home
	if c.ID.Length < 4 {
		return fmt.Errorf("id.length must be at least 4, got %d", c.ID.Length)
	}
	if c.Home.Cols < 1 || c.Home.Cols > 10 {
		return fmt.Errorf("home.cols must be between 1 and 10, got %d", c.Home.Cols)
	}
	if c.Home.PageSize < 1 {
		return fmt.Errorf("home.page_size must be at least 1, got %d", c.Home.PageSize)
	}

	// log_level
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level must be one of \"debug\", \"info\", \"warn\", \"error\", got %q", c.LogLevel)
	}

	return nil
}
