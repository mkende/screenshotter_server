package config

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server   ServerConfig   `toml:"server"`
	ID       IDConfig       `toml:"id"`
	Session  SessionConfig  `toml:"session"`
	CORS     CORSConfig     `toml:"cors"`
	Database DatabaseConfig `toml:"database"`
	Auth     AuthConfig     `toml:"auth"`
}

type ServerConfig struct {
	Domain      string `toml:"domain"`
	Listen      string `toml:"listen"`
	StoragePath string `toml:"storage_path"`
	MaxUploadMB int64  `toml:"max_upload_mb"`
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

type TailscaleConfig struct {
	ProxyIPs []string `toml:"proxy_ips"`
}

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
		Session: SessionConfig{
			TTL: tomlDuration{720 * time.Hour},
		},
		Auth: AuthConfig{
			OIDC: OIDCConfig{
				Scopes: []string{"openid", "email", "profile"},
			},
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
	if cfg.ID.Length < 4 {
		return fmt.Errorf("id.length must be at least 4")
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
		for _, cidr := range cfg.Auth.Tailscale.ProxyIPs {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("auth.tailscale.proxy_ips: invalid CIDR %q: %w", cidr, err)
			}
		}
	default:
		return fmt.Errorf("auth.backend must be 'oidc' or 'tailscale'")
	}
	return nil
}
