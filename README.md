# Screenshotter server

Go HTTP server that receives screenshots from the browser extension, stores
them on disk, and serves a simple web UI for viewing them.

## Requirements

- Go 1.23+ with CGo enabled (required by `mattn/go-sqlite3` for SQLite)
- A C compiler (`gcc` / `clang`) on the build host
- A reverse proxy that terminates TLS (nginx, Caddy, etc.)

## Build

```sh
cd server
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
- **`db.dsn`** — Path to `.sqlite` file, or a PostgreSQL connection string.
- **At least one authentication backend** — see below.
- **`jwt_secret`** — Required when OIDC is enabled. Random string ≥ 32 chars,
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
authorization-code flow with PKCE. Any spec-compliant provider works (Google,
Keycloak, GitHub via Dex, …). Register this redirect URI with your IdP:

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
# client_secret_env_var = "SCREENSHOTTER_OIDC_CLIENT_SECRET"
# scopes       = ["openid", "email", "profile"]
# groups_claim = "groups"
```

### Admins

Users are granted admin privileges via:

```toml
admin_emails = ["alice@example.com"]
admin_groups = ["screenshotter-admins"]
```

`admin_groups` matches against the OIDC groups claim or the configured proxy
groups header.

### CORS / extension ID

```toml
[cors]
extension_ids = ["abcdefghijklmnopqrstuvwxyz123456"]
```

Replace the placeholder with the real Chrome extension ID. List multiple IDs
for dev + production builds. The ID is shown at `chrome://extensions` after
loading the extension.

## Running

```sh
./screenshotter -config /etc/screenshotter/config.toml
```

The server listens on `listen_addr` (default `0.0.0.0:8080`) and expects the
reverse proxy to handle TLS.

### Reverse proxy

The session cookie is set with `Secure`, so the server must be served over
HTTPS in production. Configure your proxy to forward to `127.0.0.1:8080` (or
whatever `listen_addr` is set to).

**Caddy example** (`/etc/caddy/Caddyfile`):

```
screenshots.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

**nginx example** (`/etc/nginx/sites-available/screenshotter`):

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

Add the proxy's CIDR to `trusted_proxy` so forwarded headers are honoured.

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

## Unauthenticated routes & rate limiting

Two routes are reachable without authentication:

- `GET /{id}` — the HTML image viewer
- `GET /{id}.png` — the raw PNG

Both apply a per-IP sliding-window rate limit (`ratelimit.requests_per_second`
/ `requests_per_minute`) that also counts 404 responses, preventing ID
enumeration scraping. All other routes (upload, delete, annotate, thumbnails,
the home page when logged in) require a valid identity.

## Storage layout

```
<storage_path>/
  <id>.png          # full-size PNG
  thumbs/<id>.png   # 320px thumbnail generated at upload time
```

The `storage_path` directory and the `thumbs/` subdirectory are created
automatically on startup.

## Default values

| Setting | Default |
|---|---|
| `listen_addr` | `0.0.0.0:8080` |
| `title` | `Screenshotter` |
| `log_level` | `info` |
| `server.max_upload_mb` | `4` |
| `id.length` | `8` |
| `session.ttl` | `168h` (7 days) |
| `session.renewal_delay` | `1h` |
| `home.cols` | `5` |
| `home.page_size` | `20` |
| `ratelimit.requests_per_second` | `5` |
| `ratelimit.requests_per_minute` | `50` |
| `oidc.scopes` | `["openid", "email", "profile"]` |
| `oidc.groups_claim` | `groups` |
| `proxy_auth.user_header` | `Remote-User` |
| `proxy_auth.email_header` | `Remote-Email` |
| `proxy_auth.name_header` | `Remote-Name` |
| `proxy_auth.groups_header` | `Remote-Groups` |
