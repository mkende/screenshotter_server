package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// openSQLiteFile opens a file-backed SQLite database with a plain-path DSN,
// exactly as a production config would.
func openSQLiteFile(t *testing.T) *DB {
	t.Helper()
	d, err := Open("sqlite", filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite file database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// holdConns checks out n distinct connections from the pool and keeps them
// open until the test ends, forcing the pool to open fresh connections for
// any further work.
func holdConns(t *testing.T, d *DB, n int) []*sql.Conn {
	t.Helper()
	conns := make([]*sql.Conn, 0, n)
	for i := 0; i < n; i++ {
		c, err := d.sql.Conn(context.Background())
		if err != nil {
			t.Fatalf("checkout connection %d: %v", i, err)
		}
		t.Cleanup(func() { c.Close() })
		conns = append(conns, c)
	}
	return conns
}

// TestSQLite_ForeignKeysEnabledOnEveryPooledConnection verifies that
// foreign-key enforcement is on for every connection the pool opens, not just
// the one that ran the startup pragmas (regression: only one connection had it).
func TestSQLite_ForeignKeysEnabledOnEveryPooledConnection(t *testing.T) {
	d := openSQLiteFile(t)
	for i, c := range holdConns(t, d, 4) {
		var fk int
		if err := c.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatalf("conn %d: PRAGMA foreign_keys: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("conn %d: foreign_keys = %d, want 1", i, fk)
		}
	}
}

// TestSQLite_DeleteUserCascadesOnFreshConnection verifies the users → images
// cascade fires even when the delete runs on a connection opened after
// startup. Holding the existing connections forces DeleteUser onto a new one.
func TestSQLite_DeleteUserCascadesOnFreshConnection(t *testing.T) {
	d := openSQLiteFile(t)
	ctx := context.Background()
	ids := insertUserAndImages(t, d, "cascade@x.com", "Cascade", 3)

	// Every connection opened so far is now busy; DeleteUser must open a new one.
	holdConns(t, d, 2)

	ok, err := d.DeleteUser(ctx, "cascade@x.com")
	if err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	for _, id := range ids {
		img, err := d.GetImage(ctx, id)
		if err != nil {
			t.Fatalf("GetImage %q: %v", id, err)
		}
		if img != nil {
			t.Errorf("image %q should be cascade-deleted with the user", id)
		}
	}
}

// TestSQLite_InsertImageForUnknownOwnerFails verifies the images.owner_id
// foreign key is enforced on a fresh connection.
func TestSQLite_InsertImageForUnknownOwnerFails(t *testing.T) {
	d := openSQLiteFile(t)
	holdConns(t, d, 2)
	err := d.InsertImage(context.Background(), Image{
		ID: "fk000001", OwnerID: "nobody@x.com", FilePath: "fk000001.png",
	})
	if err == nil {
		t.Fatal("expected foreign-key violation inserting image for unknown owner")
	}
}
