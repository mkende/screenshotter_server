package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps sql.DB with typed query methods.
type DB struct {
	sql     *sql.DB
	backend string
}

// Open opens the database, runs migrations, and returns a DB.
func Open(backend, dsn string) (*DB, error) {
	driverName := backend
	if backend == "sqlite" {
		// mattn/go-sqlite3 registers as "sqlite3"
		driverName = "sqlite3"
	}
	sqlDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if backend == "sqlite" {
		if _, err := sqlDB.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("sqlite pragmas: %w", err)
		}
	}
	d := &DB{sql: sqlDB, backend: backend}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}

// q rewrites ? placeholders to $N for Postgres.
func (d *DB) q(query string) string {
	if d.backend != "postgres" {
		return query
	}
	var b strings.Builder
	n := 0
	for _, c := range query {
		if c == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func (d *DB) migrate() error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations source: %w", err)
	}
	var m *migrate.Migrate
	switch d.backend {
	case "sqlite":
		drv, err := sqlite3.WithInstance(d.sql, &sqlite3.Config{})
		if err != nil {
			return fmt.Errorf("migrate sqlite3 driver: %w", err)
		}
		m, err = migrate.NewWithInstance("iofs", src, "sqlite3", drv)
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	case "postgres":
		drv, err := postgres.WithInstance(d.sql, &postgres.Config{})
		if err != nil {
			return fmt.Errorf("migrate postgres driver: %w", err)
		}
		m, err = migrate.NewWithInstance("iofs", src, "postgres", drv)
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	default:
		return fmt.Errorf("unsupported backend %q", d.backend)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// Close closes the underlying database.
func (d *DB) Close() error { return d.sql.Close() }

// --- User queries ---

type User struct {
	ID          string
	DisplayName string
	Email       string
	CreatedAt   time.Time
}

// UpsertUser inserts or updates a user's display name and email.
func (d *DB) UpsertUser(ctx context.Context, id, displayName, email string) error {
	var query string
	switch d.backend {
	case "postgres":
		query = `
			INSERT INTO users (id, display_name, email) VALUES ($1, $2, $3)
			ON CONFLICT(id) DO UPDATE SET display_name = EXCLUDED.display_name, email = EXCLUDED.email`
	default:
		query = `
			INSERT INTO users (id, display_name, email) VALUES (?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET display_name = excluded.display_name, email = excluded.email`
	}
	if _, err := d.sql.ExecContext(ctx, query, id, displayName, email); err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

// GetUser returns a user by ID, or (nil, nil) if not found.
func (d *DB) GetUser(ctx context.Context, id string) (*User, error) {
	u := &User{}
	err := d.sql.QueryRowContext(ctx,
		d.q(`SELECT id, display_name, email, created_at FROM users WHERE id = ?`), id).
		Scan(&u.ID, &u.DisplayName, &u.Email, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// --- Image queries ---

// Image represents a stored screenshot record.
type Image struct {
	ID        string
	OwnerID   string
	Title     *string // nil means no title has been set by the user
	SourceURL string
	FilePath  string
	CreatedAt time.Time
}

// InsertImage stores a new image record.
func (d *DB) InsertImage(ctx context.Context, img Image) error {
	_, err := d.sql.ExecContext(ctx,
		d.q(`INSERT INTO images (id, owner_id, source_url, file_path) VALUES (?, ?, ?, ?)`),
		img.ID, img.OwnerID, img.SourceURL, img.FilePath)
	if err != nil {
		return fmt.Errorf("insert image: %w", err)
	}
	return nil
}

// GetImage returns an image by ID, or (nil, nil) if not found.
func (d *DB) GetImage(ctx context.Context, id string) (*Image, error) {
	img := &Image{}
	err := d.sql.QueryRowContext(ctx,
		d.q(`SELECT id, owner_id, title, source_url, file_path, created_at FROM images WHERE id = ?`), id).
		Scan(&img.ID, &img.OwnerID, &img.Title, &img.SourceURL, &img.FilePath, &img.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get image: %w", err)
	}
	return img, nil
}

// UpdateImage sets the title (nil clears it) and source URL for an image owned
// by ownerID. Returns false if not found or not owned by the caller.
func (d *DB) UpdateImage(ctx context.Context, id, ownerID string, title *string, sourceURL string) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		d.q(`UPDATE images SET title = ?, source_url = ? WHERE id = ? AND owner_id = ?`),
		title, sourceURL, id, ownerID)
	if err != nil {
		return false, fmt.Errorf("update image: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteImage removes an image owned by ownerID. Returns false if not found or not owned.
func (d *DB) DeleteImage(ctx context.Context, id, ownerID string) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		d.q(`DELETE FROM images WHERE id = ? AND owner_id = ?`), id, ownerID)
	if err != nil {
		return false, fmt.Errorf("delete image: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListRecentImages returns up to limit images for ownerID starting at the given
// offset, ordered newest first. Pass limit+1 and check len to detect a next page.
func (d *DB) ListRecentImages(ctx context.Context, ownerID string, limit, offset int) ([]Image, error) {
	rows, err := d.sql.QueryContext(ctx,
		d.q(`SELECT id, owner_id, title, source_url, file_path, created_at
		     FROM images WHERE owner_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`),
		ownerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	defer rows.Close()
	var imgs []Image
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.OwnerID, &img.Title, &img.SourceURL, &img.FilePath, &img.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan image row: %w", err)
		}
		imgs = append(imgs, img)
	}
	return imgs, rows.Err()
}
