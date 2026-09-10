# Screenshotter server

Go HTTP server that pairs with the
[Screenshotter Chrome extension](https://chromewebstore.google.com/detail/nnipkgjcfgekggpkclhdghbbfnpokdlg) to receive
browser screenshots, store them on disk, and serve a web UI for browsing and
annotating them.

The server is the backend component of Screenshotter. The Chrome extension
captures and crops screenshots in the browser, then uploads them here.

A pre-built Docker image is published at
[ghcr.io/mkende/screenshotter](https://github.com/users/mkende/packages/container/package/screenshotter).

## Docker

The fastest way to get started is with the pre-built image. Copy the example
config, fill in the required fields (see [Configuration](#configuration)),
then start the container:

```sh
cp config.example.toml config.toml
$EDITOR config.toml
docker compose up -d
```

`docker-compose.yml`:

```yaml
services:
  screenshotter:
    image: ghcr.io/mkende/screenshotter:latest
    restart: unless-stopped
    volumes:
      - ./config.toml:/etc/screenshotter/config.toml:ro
      - screenshotter-data:/var/lib/screenshotter
    ports:
      - "127.0.0.1:8080:8080"

volumes:
  screenshotter-data:
```

## Build from source

Requirements:

- Go 1.25+ with CGo enabled (required by `mattn/go-sqlite3` for SQLite)
- A C compiler (`gcc` / `clang`) on the build host

```sh
go build -o screenshotter ./cmd/screenshotter
```

This produces a single self-contained binary. Migrations run automatically on
startup; no separate migration step is needed.

## Configuration

Copy the example config and fill in the required fields:

```sh
cp config.example.toml config.toml
$EDITOR config.toml
```

### Required fields

- **`canonical_address`** — Public base URL including scheme, no trailing slash
  (e.g. `https://screenshots.example.com`). Required when OIDC is enabled.
  When set, any request arriving on a different scheme or host is redirected
  here with a 301, preserving path and query.
- **`server.storage_path`** — Directory where PNGs and thumbnails are stored.
- **`db.driver`** — `sqlite` or `postgres`.
- **`db.dsn`** — Path to the `.sqlite` file, or a PostgreSQL connection string.
  Can also be provided via `db.dsn_env_var`, which names an environment
  variable holding the DSN — useful for PostgreSQL, whose DSN embeds a
  password. The two forms are mutually exclusive; exactly one must be set.
- **At least one authentication backend** — see [Authentication backends](#authentication-backends).
- **`jwt_secret`** — Required when OIDC is enabled. Random string ≥ 32 chars;
  generate with `openssl rand -hex 32`. Can also be provided via
  `jwt_secret_env_var`.

### Authentication backends

Each backend is an independent middleware gated by its own `enabled` flag. At
least one must be enabled; several can be active at once, in which case they
run in a chain (Tailscale → ProxyAuth → OIDC → Anonymous) and the first one
to produce an identity wins.

**Anonymous** — for development or trusted private instances. Treats every
request as one shared user. Do not enable on the public internet.

```toml
[anonymous]
enabled = true
# is_admin = false  # grant the anonymous user full admin privileges
```

**Tailscale** — identity is read from `Tailscale-User-Login` and
`Tailscale-User-Name` headers injected by a trusted Tailscale proxy. Requires
`trusted_proxy` to list the CIDRs the proxy connects from.

```toml
trusted_proxy = ["100.64.0.0/10", "fd7a:115c:a1e0::/48"]

[tailscale]
enabled = true
```

**Proxy auth** — identity is read from `Remote-*` headers injected by a
trusted reverse proxy (Authelia, oauth2-proxy, …). Requires `trusted_proxy`.
Header names are overridable.

```toml
trusted_proxy = ["10.0.0.0/8"]

[proxy_auth]
enabled = true
# user_header   = "Remote-User"
# email_header  = "Remote-Email"
# name_header   = "Remote-Name"
# groups_header = "Remote-Groups"
```

**OIDC** — the server acts as an OIDC Relying Party using the
authorization-code flow. Any spec-compliant provider works (Google, Keycloak,
GitHub via Dex, …). Register this redirect URI with your IdP:

```
<canonical_address>/auth/callback
```

```toml
canonical_address = "https://screenshots.example.com"
jwt_secret        = "…32+ random chars…"

[oidc]
enabled       = true
issuer        = "https://accounts.google.com"
client_id     = "<your-client-id>"
client_secret = "<your-client-secret>"
# client_secret_env_var  = "SCREENSHOTTER_OIDC_CLIENT_SECRET"
# scopes                 = ["openid", "email", "profile"]
# groups_claim           = "groups"
# use_pkce               = false
# require_email_verified = true
```

### Admins

```toml
admin_emails = ["alice@example.com"]
admin_groups = ["screenshotter-admins"]
```

`admin_groups` matches against the OIDC groups claim or the configured proxy
groups header.

### CORS / extension ID

```toml
[cors]
extension_ids = ["nnipkgjcfgekggpkclhdghbbfnpokdlg"]
```

The default value matches the published Chrome extension. If you load the
extension unpacked (development builds), replace it with the ID shown at
`chrome://extensions`. Multiple IDs can be listed to support dev and
production builds simultaneously.

### Optional settings

- **`listen_addr`** — TCP address the server binds to. Default: `0.0.0.0:8080`.
- **`title`** — Human-readable name shown in the UI. Default: `Screenshotter`.
- **`log_level`** — Minimum log severity: `debug`, `info`, `warn`, `error`.
  Default: `info`.
- **`favicon_path`** — Path to a custom favicon file.
- **`trusted_proxy`** — CIDRs of reverse proxies whose forwarding headers are
  trusted. Required when using the Tailscale or proxy auth backends.
- **`source_url_schemes`** — Allowlist of URL schemes accepted for an image's
  source URL. Default: `["http", "https"]`. Add entries such as `file` or
  `ftp` as needed; an empty list disables source URLs. The dangerous schemes
  `javascript`, `data`, and `vbscript` are always rejected and listing one is a
  startup error.
- **`server.max_upload_mb`** — Maximum upload size in megabytes. Default: `4`.
- **`server.assets_path`** — Optional directory of static assets. When set,
  `/favicon.ico` is served from `<assets_path>/favicon.ico` unless overridden
  by `favicon_path`.
- **`server.require_auth_to_view`** — When `true`, viewing images requires an
  authenticated session. Default: `false` (anyone with the image ID can view
  it).
- **`id.length`** — Length of the random alphanumeric image ID. Default: `8`,
  minimum: `4`.
- **`session.ttl`** — Hard session expiry. Default: `168h` (7 days).
- **`session.renewal_delay`** — Minimum age before silent session renewal.
  Default: `1h`.
- **`home.cols`** — Maximum thumbnail columns in the gallery (1–10).
  Default: `5`.
- **`home.page_size`** — Screenshots shown per page. Default: `20`.
- **`ratelimit.requests_per_second`** — Max requests per source IP per second
  on public image routes. Default: `6`. Set to `0` to disable.
- **`ratelimit.requests_per_minute`** — Max requests per source IP per minute.
  Default: `60`. Set to `0` to disable.

## Running

```sh
./screenshotter -config /etc/screenshotter/config.toml
```

The server listens on `listen_addr` and expects a reverse proxy to handle TLS.
The session cookie is set with `Secure`, so HTTPS is required in production.

### Reverse proxy

Configure your proxy to forward to `127.0.0.1:8080` (or whatever
`listen_addr` is set to), and add the proxy's CIDR to `trusted_proxy` in
`config.toml` so forwarded headers are honoured.

**Caddy** (`/etc/caddy/Caddyfile`):

```
screenshots.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

**nginx** (`/etc/nginx/sites-available/screenshotter`):

```nginx
server {
    listen 443 ssl;
    server_name screenshots.example.com;

    ssl_certificate     /etc/ssl/certs/screenshots.example.com.pem;
    ssl_certificate_key /etc/ssl/private/screenshots.example.com.key;

    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        client_max_body_size 10m;
    }
}
```

### systemd service

```ini
# /etc/systemd/system/screenshotter.service
[Unit]
Description=Screenshotter server
After=network.target

[Service]
ExecStart=/usr/local/bin/screenshotter -config /etc/screenshotter/config.toml
Restart=on-failure
User=screenshotter
Group=screenshotter
# Storage and DB directories must be owned by this user.

[Install]
WantedBy=multi-user.target
```

```sh
systemctl daemon-reload
systemctl enable --now screenshotter
```
