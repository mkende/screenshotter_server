module github.com/mkende/screenshotter/server

go 1.23.0

toolchain go1.24.7

require (
	github.com/BurntSushi/toml v1.4.0
	github.com/coreos/go-oidc/v3 v3.11.0
	github.com/go-chi/chi/v5 v5.2.1
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/golang-migrate/migrate/v4 v4.18.1
	github.com/lib/pq v1.10.9
	github.com/mattn/go-sqlite3 v1.14.24
	golang.org/x/image v0.24.0
	golang.org/x/oauth2 v0.28.0
)

require (
	github.com/fergusstrange/embedded-postgres v1.34.0 // indirect
	github.com/go-jose/go-jose/v4 v4.0.2 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	golang.org/x/crypto v0.27.0 // indirect
)
