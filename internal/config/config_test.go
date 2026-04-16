package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig writes content to a temp TOML file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.toml")
	if err != nil {
		t.Fatalf("create temp config: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write config: %v", err)
	}
	f.Close()
	return f.Name()
}

// validBase returns a minimal valid config TOML string.
func validBase() string {
	return `
[server]
domain       = "https://example.com"
storage_path = "/tmp/storage"

[session]
secret = "a-secret-that-is-at-least-32-characters-long"

[database]
backend = "sqlite"
dsn     = ":memory:"

[auth]
backend = "tailscale"
`
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeConfig(t, validBase())
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Server.Domain != "https://example.com" {
		t.Errorf("unexpected domain: %q", cfg.Server.Domain)
	}
}

func TestLoad_MissingDomain(t *testing.T) {
	toml := strings.ReplaceAll(validBase(), `domain       = "https://example.com"`, "")
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "server.domain") {
		t.Errorf("expected domain error, got: %v", err)
	}
}

func TestLoad_MissingStoragePath(t *testing.T) {
	toml := strings.ReplaceAll(validBase(), `storage_path = "/tmp/storage"`, "")
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "server.storage_path") {
		t.Errorf("expected storage_path error, got: %v", err)
	}
}

func TestLoad_ShortSecret(t *testing.T) {
	toml := strings.ReplaceAll(validBase(),
		`secret = "a-secret-that-is-at-least-32-characters-long"`,
		`secret = "tooshort"`)
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "32") {
		t.Errorf("expected secret length error, got: %v", err)
	}
}

func TestLoad_MissingSecret(t *testing.T) {
	toml := strings.ReplaceAll(validBase(),
		`secret = "a-secret-that-is-at-least-32-characters-long"`, "")
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "session.secret") {
		t.Errorf("expected session.secret error, got: %v", err)
	}
}

func TestLoad_BadDuration(t *testing.T) {
	// We need to write a fresh file with the override
	content := `
[server]
domain       = "https://example.com"
storage_path = "/tmp/storage"

[session]
secret = "a-secret-that-is-at-least-32-characters-long"
ttl    = "notaduration"

[database]
backend = "sqlite"
dsn     = ":memory:"

[auth]
backend = "tailscale"
`
	path := writeConfig(t, content)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for bad duration, got nil")
	}
}

func TestLoad_TrailingSlashTrimmedFromDomain(t *testing.T) {
	toml := strings.ReplaceAll(validBase(),
		`domain       = "https://example.com"`,
		`domain       = "https://example.com/"`)
	path := writeConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasSuffix(cfg.Server.Domain, "/") {
		t.Errorf("expected trailing slash to be trimmed, got %q", cfg.Server.Domain)
	}
}

func TestLoad_BadCIDR(t *testing.T) {
	toml := strings.ReplaceAll(validBase(),
		`domain       = "https://example.com"`,
		`domain       = "https://example.com"
trusted_proxy_ips = ["not-a-cidr"]`)
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "CIDR") {
		t.Errorf("expected CIDR error, got: %v", err)
	}
}

func TestLoad_BadAuthBackend(t *testing.T) {
	toml := strings.ReplaceAll(validBase(),
		`backend = "tailscale"`,
		`backend = "unknown"`)
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "auth.backend") {
		t.Errorf("expected auth.backend error, got: %v", err)
	}
}

func TestLoad_BadDatabaseBackend(t *testing.T) {
	toml := strings.ReplaceAll(validBase(),
		`backend = "sqlite"`,
		`backend = "mysql"`)
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "database.backend") {
		t.Errorf("expected database.backend error, got: %v", err)
	}
}

func TestLoad_OIDCBackendMissingFields(t *testing.T) {
	toml := strings.ReplaceAll(validBase(),
		`backend = "tailscale"`,
		`backend = "oidc"`)
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "oidc") {
		t.Errorf("expected oidc error, got: %v", err)
	}
}

func TestLoad_Defaults(t *testing.T) {
	path := writeConfig(t, validBase())
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("expected default listen :8080, got %q", cfg.Server.Listen)
	}
	if cfg.Server.MaxUploadMB != 4 {
		t.Errorf("expected default max_upload_mb 4, got %d", cfg.Server.MaxUploadMB)
	}
	if cfg.ID.Length != 8 {
		t.Errorf("expected default id.length 8, got %d", cfg.ID.Length)
	}
	if cfg.Session.TTL.Duration != 720*time.Hour {
		t.Errorf("expected default ttl 720h, got %v", cfg.Session.TTL.Duration)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
