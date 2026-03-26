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

| Field | Description |
|---|---|
| `server.domain` | Public HTTPS base URL, no trailing slash (e.g. `https://screenshots.example.com`) |
| `server.storage_path` | Directory where PNGs and thumbnails are stored |
| `session.secret` | Random string ≥ 32 characters used to sign JWT cookies — generate with `openssl rand -hex 32` |
| `database.backend` | `sqlite` or `postgres` |
| `database.dsn` | Path to `.sqlite` file, or a PostgreSQL connection string |
| `auth.backend` | `oidc` or `tailscale` |

### OIDC auth

Set `auth.backend = "oidc"` and fill in the `[auth.oidc]` section.

Register the following **redirect URI** with your identity provider:

```
https://<your-domain>/auth/callback
```

The server uses the standard authorization-code flow with PKCE. Any
spec-compliant OIDC provider works (Google, GitHub via Dex, Keycloak, …).

### Tailscale auth

Set `auth.backend = "tailscale"` and point your Tailscale serve/funnel config
at the server. Identity is read from the `Tailscale-User-Login` and
`Tailscale-User-Name` headers injected by the Tailscale proxy.

For additional security, restrict header trust to specific source CIDRs:

```toml
[auth.tailscale]
proxy_ips = ["100.64.0.0/10", "fd7a:115c:a1e0::/48"]
```

Leave `proxy_ips` empty to trust all source addresses, which is safe when the
server is not reachable from the public internet.

### CORS / extension ID

```toml
[cors]
extension_ids = ["abcdefghijklmnopqrstuvwxyz123456"]
```

Replace the placeholder with the real Chrome extension ID. You can list
multiple IDs (e.g. dev and production builds). The extension ID is shown at
`chrome://extensions` after loading the extension.

## Running

```sh
./screenshotter -config /etc/screenshotter/config.toml
```

The server listens on `server.listen` (default `:8080`) and expects the reverse
proxy to handle TLS.

### Reverse proxy

The server must be served over HTTPS because the session cookie is set with
`Secure`. Configure your proxy to forward to `127.0.0.1:8080` (or whatever
`server.listen` is set to).

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
| `server.listen` | `:8080` |
| `server.max_upload_mb` | `4` |
| `id.length` | `8` |
| `session.ttl` | `720h` (30 days) |
| `auth.oidc.scopes` | `["openid", "email", "profile"]` |
