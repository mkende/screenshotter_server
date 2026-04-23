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

// validBase returns a minimal valid config TOML string with a single auth
// provider (anonymous) enabled so validation succeeds.
func validBase() string {
	return validBaseWith("")
}

// validBaseWith returns validBase with extra top-level keys injected before
// the first table, so appended table sections can follow safely.
func validBaseWith(topLevelExtras string) string {
	return `
canonical_address = "https://example.com"
` + topLevelExtras + `
[server]
storage_path = "/tmp/storage"

[anonymous]
enabled = true

[db]
driver = "sqlite"
dsn    = ":memory:"
`
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeConfig(t, validBase())
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.CanonicalAddress != "https://example.com" {
		t.Errorf("unexpected canonical_address: %q", cfg.CanonicalAddress)
	}
	if cfg.CanonicalHost() != "example.com" {
		t.Errorf("unexpected canonical host: %q", cfg.CanonicalHost())
	}
	if cfg.CanonicalScheme() != "https" {
		t.Errorf("unexpected canonical scheme: %q", cfg.CanonicalScheme())
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

func TestLoad_NoAuthProvider(t *testing.T) {
	toml := strings.ReplaceAll(validBase(), "[anonymous]\nenabled = true\n", "")
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "authentication provider") {
		t.Errorf("expected no-auth-provider error, got: %v", err)
	}
}

func TestLoad_TrailingSlashTrimmedFromCanonical(t *testing.T) {
	toml := strings.ReplaceAll(validBase(),
		`canonical_address = "https://example.com"`,
		`canonical_address = "https://example.com/"`)
	path := writeConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasSuffix(cfg.CanonicalAddress, "/") {
		t.Errorf("expected trailing slash to be trimmed, got %q", cfg.CanonicalAddress)
	}
}

func TestLoad_BadCIDR(t *testing.T) {
	toml := validBase() + `
trusted_proxy = ["not-a-cidr"]
`
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "CIDR") {
		t.Errorf("expected CIDR error, got: %v", err)
	}
}

func TestLoad_BadDatabaseDriver(t *testing.T) {
	toml := strings.ReplaceAll(validBase(), `driver = "sqlite"`, `driver = "mysql"`)
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "db.driver") {
		t.Errorf("expected db.driver error, got: %v", err)
	}
}

func TestLoad_OIDCEnabledMissingFields(t *testing.T) {
	toml := validBaseWith(`jwt_secret = "a-secret-that-is-at-least-32-characters-long"
`) + `
[oidc]
enabled = true
`
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "oidc") {
		t.Errorf("expected oidc error, got: %v", err)
	}
}

func TestLoad_OIDCWithoutCanonical(t *testing.T) {
	base := validBaseWith(`jwt_secret = "a-secret-that-is-at-least-32-characters-long"
`)
	toml := strings.ReplaceAll(base, `canonical_address = "https://example.com"`, "") + `
[oidc]
enabled       = true
issuer        = "https://issuer.example.com"
client_id     = "foo"
client_secret = "bar"
`
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "canonical_address") {
		t.Errorf("expected canonical_address error, got: %v", err)
	}
}

func TestLoad_OIDCMissingJWT(t *testing.T) {
	toml := validBase() + `
[oidc]
enabled       = true
issuer        = "https://issuer.example.com"
client_id     = "foo"
client_secret = "bar"
`
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "jwt_secret") {
		t.Errorf("expected jwt_secret error, got: %v", err)
	}
}

func TestLoad_TailscaleWithoutTrustedProxy(t *testing.T) {
	toml := validBase() + `
[tailscale]
enabled = true
`
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "trusted_proxy") {
		t.Errorf("expected trusted_proxy error, got: %v", err)
	}
}

func TestLoad_ProxyAuthWithoutTrustedProxy(t *testing.T) {
	toml := validBase() + `
[proxy_auth]
enabled = true
`
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "trusted_proxy") {
		t.Errorf("expected trusted_proxy error, got: %v", err)
	}
}

func TestLoad_Defaults(t *testing.T) {
	path := writeConfig(t, validBase())
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:8080" {
		t.Errorf("expected default listen 0.0.0.0:8080, got %q", cfg.ListenAddr)
	}
	if cfg.Server.MaxUploadMB != 4 {
		t.Errorf("expected default max_upload_mb 4, got %d", cfg.Server.MaxUploadMB)
	}
	if cfg.ID.Length != 8 {
		t.Errorf("expected default id.length 8, got %d", cfg.ID.Length)
	}
	if cfg.Session.TTL.Duration != 7*24*time.Hour {
		t.Errorf("expected default ttl 168h, got %v", cfg.Session.TTL.Duration)
	}
	if cfg.Session.RenewalDelay.Duration != time.Hour {
		t.Errorf("expected default renewal_delay 1h, got %v", cfg.Session.RenewalDelay.Duration)
	}
	if cfg.Title != "Screenshotter" {
		t.Errorf("expected default Title Screenshotter, got %q", cfg.Title)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected default log_level info, got %q", cfg.LogLevel)
	}
	if cfg.ProxyAuth.UserHeader != "Remote-User" {
		t.Errorf("expected default proxy_auth.user_header Remote-User, got %q", cfg.ProxyAuth.UserHeader)
	}
	if len(cfg.OIDC.Scopes) == 0 || cfg.OIDC.Scopes[0] != "openid" {
		t.Errorf("expected default oidc scopes to start with openid, got %v", cfg.OIDC.Scopes)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoad_JWTSecretFromEnv(t *testing.T) {
	t.Setenv("SS_TEST_JWT", "a-secret-that-is-at-least-32-characters-long")
	toml := validBaseWith(`jwt_secret_env_var = "SS_TEST_JWT"
`) + `
[oidc]
enabled       = true
issuer        = "https://issuer.example.com"
client_id     = "foo"
client_secret = "bar"
`
	path := writeConfig(t, toml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JWTSecret == "" {
		t.Error("expected JWTSecret to be resolved from env")
	}
}

func TestLoad_JWTSecretBothFormsError(t *testing.T) {
	toml := validBaseWith(`jwt_secret         = "a-secret-that-is-at-least-32-characters-long"
jwt_secret_env_var = "SS_TEST_JWT"
`)
	path := writeConfig(t, toml)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error when both jwt_secret and jwt_secret_env_var are set")
	}
}
