package db

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// testPostgresDSN is set by TestMain when embedded Postgres starts successfully,
// or when TEST_POSTGRES_DSN is provided externally.
var testPostgresDSN string

// TestMain starts an embedded Postgres instance shared across all tests in this
// package, then runs the tests.  If Postgres cannot start the tests still run
// against SQLite only.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	// Allow an external DSN override (e.g. from CI with a managed Postgres).
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		testPostgresDSN = dsn
		return m.Run()
	}

	port, err := getFreePort()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: find free port: %v; running SQLite tests only\n", err)
		return m.Run()
	}

	tmpDir, err := os.MkdirTemp("", "embpg-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: create temp dir: %v; running SQLite tests only\n", err)
		return m.Run()
	}
	defer os.RemoveAll(tmpDir)

	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Username("test").
		Password("test").
		Database("test").
		Port(uint32(port)).
		RuntimePath(tmpDir))

	if err := pg.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: start embedded postgres: %v; running SQLite tests only\n", err)
		return m.Run()
	}
	defer pg.Stop()

	testPostgresDSN = fmt.Sprintf("host=localhost port=%d user=test password=test dbname=test sslmode=disable", port)
	return m.Run()
}

func strPtr(s string) *string { return &s }

func getFreePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

// testBackend pairs a backend name with an open *DB for parameterised testing.
type testBackend struct {
	name string
	db   *DB
}

// allBackends returns backends to run each test against.
// SQLite (in-memory) is always included.  Postgres is added when the
// TEST_POSTGRES_DSN environment variable is set.
func allBackends(t *testing.T) []testBackend {
	t.Helper()
	bs := []testBackend{{"sqlite", openSQLiteDB(t)}}
	if testPostgresDSN != "" {
		bs = append(bs, testBackend{"postgres", openPostgresDB(t, testPostgresDSN)})
	}
	return bs
}

func openSQLiteDB(t *testing.T) *DB {
	t.Helper()
	// No _foreign_keys DSN parameter: the tests must exercise the same
	// foreign-key setup that production DSNs (a plain file path) get.
	d, err := Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open sqlite test database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func openPostgresDB(t *testing.T, dsn string) *DB {
	t.Helper()
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres test database: %v", err)
	}
	t.Cleanup(func() {
		d.sql.Exec("TRUNCATE images, users CASCADE") //nolint:errcheck
		d.Close()
	})
	return d
}

func TestUpsertUser_InsertAndRetrieve(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			if err := b.db.UpsertUser(ctx, "alice@example.com", "Alice", "https://ex/a.png"); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			u, err := b.db.GetUser(ctx, "alice@example.com")
			if err != nil {
				t.Fatalf("GetUser: %v", err)
			}
			if u == nil {
				t.Fatal("expected user, got nil")
			}
			if u.Email != "alice@example.com" {
				t.Errorf("Email: got %q, want %q", u.Email, "alice@example.com")
			}
			if u.DisplayName != "Alice" {
				t.Errorf("DisplayName: got %q, want %q", u.DisplayName, "Alice")
			}
			if u.AvatarURL != "https://ex/a.png" {
				t.Errorf("AvatarURL: got %q, want %q", u.AvatarURL, "https://ex/a.png")
			}
		})
	}
}

func TestUpsertUser_UpdatesExistingUser(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			if err := b.db.UpsertUser(ctx, "bob@example.com", "Bob", ""); err != nil {
				t.Fatalf("UpsertUser (insert): %v", err)
			}
			if err := b.db.UpsertUser(ctx, "bob@example.com", "Robert", "https://ex/b.png"); err != nil {
				t.Fatalf("UpsertUser (update): %v", err)
			}

			u, err := b.db.GetUser(ctx, "bob@example.com")
			if err != nil {
				t.Fatalf("GetUser: %v", err)
			}
			if u.DisplayName != "Robert" {
				t.Errorf("DisplayName after update: got %q, want %q", u.DisplayName, "Robert")
			}
			if u.AvatarURL != "https://ex/b.png" {
				t.Errorf("AvatarURL after update: got %q, want %q", u.AvatarURL, "https://ex/b.png")
			}
		})
	}
}

func TestGetUser_NotFound_ReturnsNil(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			u, err := b.db.GetUser(context.Background(), "doesnotexist")
			if err != nil {
				t.Fatalf("GetUser: %v", err)
			}
			if u != nil {
				t.Errorf("expected nil user for unknown ID, got %+v", u)
			}
		})
	}
}

func TestInsertImage_AndGetImage(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			if err := b.db.UpsertUser(ctx, "owner1", "Owner", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			img := Image{
				ID:        "img001",
				OwnerID:   "owner1",
				SourceURL: strPtr("https://example.com/page"),
				FilePath:  "img001.png",
			}
			if err := b.db.InsertImage(ctx, img); err != nil {
				t.Fatalf("InsertImage: %v", err)
			}

			got, err := b.db.GetImage(ctx, "img001")
			if err != nil {
				t.Fatalf("GetImage: %v", err)
			}
			if got == nil {
				t.Fatal("expected image, got nil")
			}
			if got.ID != img.ID {
				t.Errorf("ID: got %q, want %q", got.ID, img.ID)
			}
			if got.OwnerID != img.OwnerID {
				t.Errorf("OwnerID: got %q, want %q", got.OwnerID, img.OwnerID)
			}
			wantURL := ""
			if img.SourceURL != nil {
				wantURL = *img.SourceURL
			}
			gotURL := ""
			if got.SourceURL != nil {
				gotURL = *got.SourceURL
			}
			if gotURL != wantURL {
				t.Errorf("SourceURL: got %q, want %q", gotURL, wantURL)
			}
			if got.FilePath != img.FilePath {
				t.Errorf("FilePath: got %q, want %q", got.FilePath, img.FilePath)
			}
		})
	}
}

func TestGetImage_NotFound_ReturnsNil(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			got, err := b.db.GetImage(context.Background(), "nonexistent")
			if err != nil {
				t.Fatalf("GetImage: %v", err)
			}
			if got != nil {
				t.Errorf("expected nil for unknown image ID, got %+v", got)
			}
		})
	}
}

func TestDeleteImage_OwnerCanDelete(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			if err := b.db.UpsertUser(ctx, "owner2", "Owner2", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			if err := b.db.InsertImage(ctx, Image{ID: "del1", OwnerID: "owner2", SourceURL: strPtr("u"), FilePath: "del1.png"}); err != nil {
				t.Fatalf("InsertImage: %v", err)
			}

			deleted, err := b.db.DeleteImage(ctx, "del1", "owner2")
			if err != nil {
				t.Fatalf("DeleteImage: %v", err)
			}
			if !deleted {
				t.Error("expected deleted=true for owner deleting own image")
			}

			got, err := b.db.GetImage(ctx, "del1")
			if err != nil {
				t.Fatalf("GetImage after delete: %v", err)
			}
			if got != nil {
				t.Error("image should be gone after deletion")
			}
		})
	}
}

func TestDeleteImage_NonOwnerCannotDelete(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			if err := b.db.UpsertUser(ctx, "owner3", "Owner3", ""); err != nil {
				t.Fatalf("UpsertUser owner3: %v", err)
			}
			if err := b.db.UpsertUser(ctx, "other3", "Other3", ""); err != nil {
				t.Fatalf("UpsertUser other3: %v", err)
			}
			if err := b.db.InsertImage(ctx, Image{ID: "del2", OwnerID: "owner3", SourceURL: strPtr("u"), FilePath: "del2.png"}); err != nil {
				t.Fatalf("InsertImage: %v", err)
			}

			deleted, err := b.db.DeleteImage(ctx, "del2", "other3")
			if err != nil {
				t.Fatalf("DeleteImage: %v", err)
			}
			if deleted {
				t.Error("expected deleted=false when non-owner attempts deletion")
			}
		})
	}
}

func TestDeleteImage_NotFound_ReturnsFalse(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			deleted, err := b.db.DeleteImage(context.Background(), "ghost", "anyuser")
			if err != nil {
				t.Fatalf("DeleteImage: %v", err)
			}
			if deleted {
				t.Error("expected deleted=false for non-existent image")
			}
		})
	}
}

func TestListRecentImages_OrderedNewestFirst(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			if err := b.db.UpsertUser(ctx, "lister", "Lister", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			// Insert with explicit timestamps so ordering is deterministic regardless
			// of SQLite's second-granularity CURRENT_TIMESTAMP.
			rows := []struct {
				id string
				ts string
			}{
				{"img-a", "2024-01-01 10:00:00"},
				{"img-b", "2024-01-01 10:00:01"},
				{"img-c", "2024-01-01 10:00:02"},
			}
			for _, row := range rows {
				_, err := b.db.sql.ExecContext(ctx,
					b.db.q(`INSERT INTO images (id, owner_id, source_url, file_path, created_at) VALUES (?, ?, ?, ?, ?)`),
					row.id, "lister", "u", row.id+".png", row.ts)
				if err != nil {
					t.Fatalf("insert image %q: %v", row.id, err)
				}
			}

			imgs, err := b.db.ListRecentImages(ctx, "lister", 10, 0)
			if err != nil {
				t.Fatalf("ListRecentImages: %v", err)
			}
			if len(imgs) != 3 {
				t.Fatalf("expected 3 images, got %d", len(imgs))
			}
			// Newest first: img-c should be first.
			if imgs[0].ID != "img-c" {
				t.Errorf("first image should be newest (img-c), got %q", imgs[0].ID)
			}
			if imgs[2].ID != "img-a" {
				t.Errorf("last image should be oldest (img-a), got %q", imgs[2].ID)
			}
		})
	}
}

func TestListRecentImages_LimitRespected(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			if err := b.db.UpsertUser(ctx, "limiter", "Limiter", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			for i := 0; i < 5; i++ {
				id := string(rune('a' + i))
				if err := b.db.InsertImage(ctx, Image{ID: id, OwnerID: "limiter", SourceURL: strPtr("u"), FilePath: id + ".png"}); err != nil {
					t.Fatalf("InsertImage %q: %v", id, err)
				}
			}

			imgs, err := b.db.ListRecentImages(ctx, "limiter", 3, 0)
			if err != nil {
				t.Fatalf("ListRecentImages: %v", err)
			}
			if len(imgs) != 3 {
				t.Errorf("expected 3 images with limit=3, got %d", len(imgs))
			}
		})
	}
}

func TestListRecentImages_OtherUsersNotReturned(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			if err := b.db.UpsertUser(ctx, "user-x", "X", ""); err != nil {
				t.Fatalf("UpsertUser user-x: %v", err)
			}
			if err := b.db.UpsertUser(ctx, "user-y", "Y", ""); err != nil {
				t.Fatalf("UpsertUser user-y: %v", err)
			}
			if err := b.db.InsertImage(ctx, Image{ID: "ximg", OwnerID: "user-x", SourceURL: strPtr("u"), FilePath: "ximg.png"}); err != nil {
				t.Fatalf("InsertImage: %v", err)
			}

			imgs, err := b.db.ListRecentImages(ctx, "user-y", 10, 0)
			if err != nil {
				t.Fatalf("ListRecentImages: %v", err)
			}
			if len(imgs) != 0 {
				t.Errorf("expected 0 images for user-y, got %d", len(imgs))
			}
		})
	}
}
