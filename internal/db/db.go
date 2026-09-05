package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/lib/pq"
	sqlite "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// sqliteDriverName is the database/sql driver name registered by init for
// SQLite connections with foreign-key enforcement enabled.
const sqliteDriverName = "sqlite3_fk"

func init() {
	// PRAGMA foreign_keys is per-connection, so enabling it with a one-off
	// Exec on the pool only affects whichever connection happens to run it;
	// ON DELETE CASCADE (users → images) would then silently not fire on the
	// others. A connect hook enables it on every connection the pool opens.
	sql.Register(sqliteDriverName, &sqlite.SQLiteDriver{
		ConnectHook: func(c *sqlite.SQLiteConn) error {
			_, err := c.Exec("PRAGMA foreign_keys = ON", nil)
			return err
		},
	})
}

// DB wraps sql.DB with typed query methods.
type DB struct {
	sql     *sql.DB
	backend string
}

// Open opens the database, runs migrations, and returns a DB.
func Open(backend, dsn string) (*DB, error) {
	driverName := backend
	if backend == "sqlite" {
		driverName = sqliteDriverName
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
		// The journal mode is stored in the database file, so setting it once
		// is enough (unlike foreign_keys, see init).
		if _, err := sqlDB.Exec(`PRAGMA journal_mode=WAL`); err != nil {
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

// User is the stored representation of an authenticated user. The id column
// now holds the user's email address (the canonical identifier used
// throughout the application); the legacy email column is kept in sync.
type User struct {
	Email       string
	DisplayName string
	AvatarURL   string
	CreatedAt   time.Time
}

// UpsertUser inserts or updates a user's display name and avatar URL keyed by
// email. The email is stored both as the primary key (id column) and in the
// email column.
func (d *DB) UpsertUser(ctx context.Context, email, displayName, avatarURL string) error {
	var query string
	switch d.backend {
	case "postgres":
		query = `
			INSERT INTO users (id, display_name, email, avatar_url) VALUES ($1, $2, $3, $4)
			ON CONFLICT(id) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				email        = EXCLUDED.email,
				avatar_url   = EXCLUDED.avatar_url`
	default:
		query = `
			INSERT INTO users (id, display_name, email, avatar_url) VALUES (?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				display_name = excluded.display_name,
				email        = excluded.email,
				avatar_url   = excluded.avatar_url`
	}
	if _, err := d.sql.ExecContext(ctx, query, email, displayName, email, avatarURL); err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

// GetUser returns a user by email, or (nil, nil) if not found.
func (d *DB) GetUser(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := d.sql.QueryRowContext(ctx,
		d.q(`SELECT id, display_name, avatar_url, created_at FROM users WHERE id = ?`), email).
		Scan(&u.Email, &u.DisplayName, &u.AvatarURL, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// ErrDuplicateID is returned by InsertImage when the generated ID already exists.
var ErrDuplicateID = errors.New("image ID already exists")

// isDuplicateKeyErr reports whether err is a unique-constraint violation from
// either the SQLite or Postgres driver.
func isDuplicateKeyErr(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	var sqErr sqlite.Error
	if errors.As(err, &sqErr) {
		return sqErr.Code == sqlite.ErrConstraint
	}
	return false
}

// --- Image queries ---

// Image represents a stored screenshot record.
type Image struct {
	ID        string
	OwnerID   string
	Title     *string // nil means no title has been set by the user
	SourceURL *string // nil means no URL has been set (e.g. direct upload)
	FilePath  string
	CreatedAt time.Time
}

// InsertImage stores a new image record. Returns ErrDuplicateID on PK collision.
func (d *DB) InsertImage(ctx context.Context, img Image) error {
	_, err := d.sql.ExecContext(ctx,
		d.q(`INSERT INTO images (id, owner_id, source_url, file_path) VALUES (?, ?, ?, ?)`),
		img.ID, img.OwnerID, img.SourceURL, img.FilePath)
	if err != nil {
		if isDuplicateKeyErr(err) {
			return ErrDuplicateID
		}
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

// UpdateImage sets the title (nil clears it) and source URL (nil clears it) for
// an image owned by ownerID. Returns false if not found or not owned by the caller.
func (d *DB) UpdateImage(ctx context.Context, id, ownerID string, title *string, sourceURL *string) (bool, error) {
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

// UserWithStats embeds User with an aggregate image count.
type UserWithStats struct {
	User
	ImageCount int
}

// ListUsers returns users matching search (case-insensitive substring of email
// or display name), ordered by creation date descending and paginated.
// total is the count of all matching users (before pagination).
// Pass limit+1 and check len to detect a next page.
func (d *DB) ListUsers(ctx context.Context, search string, limit, offset int) (users []UserWithStats, total int, err error) {
	escaped := strings.NewReplacer(`%`, `\%`, `_`, `\_`).Replace(search)
	pattern := "%" + strings.ToLower(escaped) + "%"

	countQ := d.q(`SELECT COUNT(*) FROM users WHERE lower(id) LIKE ? ESCAPE '\' OR lower(display_name) LIKE ? ESCAPE '\'`)
	if err = d.sql.QueryRowContext(ctx, countQ, pattern, pattern).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	listQ := d.q(`
		SELECT u.id, u.display_name, u.avatar_url, u.created_at, COUNT(i.id) AS image_count
		FROM users u
		LEFT JOIN images i ON i.owner_id = u.id
		WHERE lower(u.id) LIKE ? ESCAPE '\' OR lower(u.display_name) LIKE ? ESCAPE '\'
		GROUP BY u.id
		ORDER BY u.created_at DESC
		LIMIT ? OFFSET ?`)
	rows, err := d.sql.QueryContext(ctx, listQ, pattern, pattern, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var u UserWithStats
		if err = rows.Scan(&u.Email, &u.DisplayName, &u.AvatarURL, &u.CreatedAt, &u.ImageCount); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

// GetUserWithStats returns a user by email with their image count, or (nil, nil) if not found.
func (d *DB) GetUserWithStats(ctx context.Context, email string) (*UserWithStats, error) {
	u := &UserWithStats{}
	err := d.sql.QueryRowContext(ctx, d.q(`
		SELECT u.id, u.display_name, u.avatar_url, u.created_at, COUNT(i.id) AS image_count
		FROM users u
		LEFT JOIN images i ON i.owner_id = u.id
		WHERE u.id = ?
		GROUP BY u.id`), email).
		Scan(&u.Email, &u.DisplayName, &u.AvatarURL, &u.CreatedAt, &u.ImageCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user with stats: %w", err)
	}
	return u, nil
}

// ReassignImage changes the owner of imageID to newOwnerEmail without checking
// the current owner. Returns false if the image does not exist.
func (d *DB) ReassignImage(ctx context.Context, imageID, newOwnerEmail string) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		d.q(`UPDATE images SET owner_id = ? WHERE id = ?`),
		newOwnerEmail, imageID)
	if err != nil {
		return false, fmt.Errorf("reassign image: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReassignAllImages moves every image owned by fromEmail to toEmail and
// returns the number of images moved.
func (d *DB) ReassignAllImages(ctx context.Context, fromEmail, toEmail string) (int64, error) {
	res, err := d.sql.ExecContext(ctx,
		d.q(`UPDATE images SET owner_id = ? WHERE owner_id = ?`),
		toEmail, fromEmail)
	if err != nil {
		return 0, fmt.Errorf("reassign all images: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// AdminDeleteImage removes an image by ID without checking ownership.
// Returns false if the image was not found.
func (d *DB) AdminDeleteImage(ctx context.Context, id string) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		d.q(`DELETE FROM images WHERE id = ?`), id)
	if err != nil {
		return false, fmt.Errorf("admin delete image: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListImageIDsByOwner returns all image IDs belonging to email.
// Used to collect IDs for storage cleanup before deleting a user.
func (d *DB) ListImageIDsByOwner(ctx context.Context, email string) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx,
		d.q(`SELECT id FROM images WHERE owner_id = ?`), email)
	if err != nil {
		return nil, fmt.Errorf("list image ids: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan image id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DeleteUser removes a user by email. The ON DELETE CASCADE constraint
// automatically removes all their images from the DB. Callers must call
// ListImageIDsByOwner first and clean up storage files afterwards.
// Returns false if the user was not found.
func (d *DB) DeleteUser(ctx context.Context, email string) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		d.q(`DELETE FROM users WHERE id = ?`), email)
	if err != nil {
		return false, fmt.Errorf("delete user: %w", err)
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
