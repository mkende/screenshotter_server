package config

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server    ServerConfig    `toml:"server"`
	ID        IDConfig        `toml:"id"`
	Home      HomeConfig      `toml:"home"`
	Session   SessionConfig   `toml:"session"`
	CORS      CORSConfig      `toml:"cors"`
	Database  DatabaseConfig  `toml:"database"`
	Auth      AuthConfig      `toml:"auth"`
	RateLimit RateLimitConfig `toml:"ratelimit"`
}

// RateLimitConfig controls the built-in per-IP rate limiter.
type RateLimitConfig struct {
	// RequestsPerSecond is the maximum number of requests allowed per IP per
	// second (default: 5).  Set to 0 to disable the per-second limit.
	RequestsPerSecond int `toml:"requests_per_second"`
	// RequestsPerMinute is the maximum number of requests allowed per IP per
	// minute (default: 50).  Set to 0 to disable the per-minute limit.
	RequestsPerMinute int `toml:"requests_per_minute"`
}

// HomeConfig controls the appearance of the home page screenshot gallery.
type HomeConfig struct {
	// Cols is the maximum number of thumbnail columns (1–10). Default: 5.
	Cols int `toml:"cols"`
	// PageSize is the number of screenshots shown per page. Default: 20.
	PageSize int `toml:"page_size"`
}

type ServerConfig struct {
	Domain          string   `toml:"domain"`
	Listen          string   `toml:"listen"`
	StoragePath     string   `toml:"storage_path"`
	MaxUploadMB     int64    `toml:"max_upload_mb"`
	// TrustedProxyIPs is the list of CIDRs whose X-Forwarded-For / X-Real-IP
	// headers are trusted for real-IP resolution.  Also used by the Tailscale
	// auth backend to validate that the Tailscale sidecar is the peer.
	TrustedProxyIPs []string `toml:"trusted_proxy_ips"`
}

type IDConfig struct {
	Length int `toml:"length"`
}

type SessionConfig struct {
	Secret string        `toml:"secret"`
	TTL    tomlDuration  `toml:"ttl"`
}

// tomlDuration is a time.Duration that unmarshals from a TOML string like "720h".
type tomlDuration struct{ time.Duration }

// ExportTomlDuration wraps d in a tomlDuration. Used only in tests.
func ExportTomlDuration(d time.Duration) tomlDuration { return tomlDuration{d} }

func (d *tomlDuration) UnmarshalText(b []byte) error {
	dur, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", b, err)
	}
	d.Duration = dur
	return nil
}

type CORSConfig struct {
	ExtensionIDs []string `toml:"extension_ids"`
}

type DatabaseConfig struct {
	Backend string `toml:"backend"`
	DSN     string `toml:"dsn"`
}

type AuthConfig struct {
	Backend   string          `toml:"backend"`
	OIDC      OIDCConfig      `toml:"oidc"`
	Tailscale TailscaleConfig `toml:"tailscale"`
}

type OIDCConfig struct {
	IssuerURL    string   `toml:"issuer_url"`
	ClientID     string   `toml:"client_id"`
	ClientSecret string   `toml:"client_secret"`
	Scopes       []string `toml:"scopes"`
}

// TailscaleConfig holds Tailscale auth backend settings.
// The list of trusted proxy IPs is shared with the server config
// (server.trusted_proxy_ips) rather than being duplicated here.
type TailscaleConfig struct{}

// Load parses the TOML config at path, applies defaults, and validates it.
func Load(path string) (*Config, error) {
	cfg := defaults()
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Listen:      ":8080",
			MaxUploadMB: 4,
		},
		ID: IDConfig{
			Length: 8,
		},
		Home: HomeConfig{
			Cols:     5,
			PageSize: 20,
		},
		Session: SessionConfig{
			TTL: tomlDuration{720 * time.Hour},
		},
		Auth: AuthConfig{
			OIDC: OIDCConfig{
				Scopes: []string{"openid", "email", "profile"},
			},
		},
		RateLimit: RateLimitConfig{
			RequestsPerSecond: 5,
			RequestsPerMinute: 50,
		},
	}
}

func validate(cfg *Config) error {
	if cfg.Server.Domain == "" {
		return fmt.Errorf("server.domain is required")
	}
	cfg.Server.Domain = strings.TrimRight(cfg.Server.Domain, "/")
	if cfg.Server.StoragePath == "" {
		return fmt.Errorf("server.storage_path is required")
	}
	if cfg.Server.MaxUploadMB <= 0 {
		return fmt.Errorf("server.max_upload_mb must be positive")
	}
	for _, cidr := range cfg.Server.TrustedProxyIPs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("server.trusted_proxy_ips: invalid CIDR %q: %w", cidr, err)
		}
	}
	if cfg.ID.Length < 4 {
		return fmt.Errorf("id.length must be at least 4")
	}
	if cfg.Home.Cols < 1 || cfg.Home.Cols > 10 {
		return fmt.Errorf("home.cols must be between 1 and 10")
	}
	if cfg.Home.PageSize < 1 {
		return fmt.Errorf("home.page_size must be at least 1")
	}
	if cfg.Session.Secret == "" {
		return fmt.Errorf("session.secret is required")
	}
	if len(cfg.Session.Secret) < 32 {
		return fmt.Errorf("session.secret must be at least 32 characters")
	}
	if cfg.Database.Backend != "sqlite" && cfg.Database.Backend != "postgres" {
		return fmt.Errorf("database.backend must be 'sqlite' or 'postgres'")
	}
	if cfg.Database.DSN == "" {
		return fmt.Errorf("database.dsn is required")
	}
	switch cfg.Auth.Backend {
	case "oidc":
		o := cfg.Auth.OIDC
		if o.IssuerURL == "" || o.ClientID == "" || o.ClientSecret == "" {
			return fmt.Errorf("auth.oidc: issuer_url, client_id, and client_secret are all required")
		}
	case "tailscale":
		// No additional config required; trusted proxy IPs come from
		// server.trusted_proxy_ips.
	case "anonymous":
		// No additional config required; every request is authenticated as a
		// fixed anonymous user. Intended for local testing only.
	default:
		return fmt.Errorf("auth.backend must be 'oidc', 'tailscale', or 'anonymous'")
	}
	return nil
}
