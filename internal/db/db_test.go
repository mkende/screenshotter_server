package db

import (
	"context"
	"testing"
)

// openTestDB opens an in-memory SQLite database for testing, running all migrations.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	// Use a unique name per test to avoid shared state between parallel tests.
	// file::memory: with cache=shared requires a unique name per connection group.
	d, err := Open("sqlite", "file::memory:?cache=shared&_foreign_keys=on")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestUpsertUser_InsertAndRetrieve(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertUser(ctx, "u1", "Alice", "alice@example.com"); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	u, err := d.GetUser(ctx, "u1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u == nil {
		t.Fatal("expected user, got nil")
	}
	if u.ID != "u1" {
		t.Errorf("ID: got %q, want %q", u.ID, "u1")
	}
	if u.DisplayName != "Alice" {
		t.Errorf("DisplayName: got %q, want %q", u.DisplayName, "Alice")
	}
	if u.Email != "alice@example.com" {
		t.Errorf("Email: got %q, want %q", u.Email, "alice@example.com")
	}
}

func TestUpsertUser_UpdatesExistingUser(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertUser(ctx, "u2", "Bob", "bob@example.com"); err != nil {
		t.Fatalf("UpsertUser (insert): %v", err)
	}
	// Update the same user with new display name and email.
	if err := d.UpsertUser(ctx, "u2", "Robert", "robert@example.com"); err != nil {
		t.Fatalf("UpsertUser (update): %v", err)
	}

	u, err := d.GetUser(ctx, "u2")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.DisplayName != "Robert" {
		t.Errorf("DisplayName after update: got %q, want %q", u.DisplayName, "Robert")
	}
}

func TestGetUser_NotFound_ReturnsNil(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	u, err := d.GetUser(ctx, "doesnotexist")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u != nil {
		t.Errorf("expected nil user for unknown ID, got %+v", u)
	}
}

func TestInsertImage_AndGetImage(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Create the owning user first (foreign key).
	if err := d.UpsertUser(ctx, "owner1", "Owner", "owner@example.com"); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	img := Image{
		ID:        "img001",
		OwnerID:   "owner1",
		SourceURL: "https://example.com/page",
		FilePath:  "img001.png",
	}
	if err := d.InsertImage(ctx, img); err != nil {
		t.Fatalf("InsertImage: %v", err)
	}

	got, err := d.GetImage(ctx, "img001")
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
	if got.SourceURL != img.SourceURL {
		t.Errorf("SourceURL: got %q, want %q", got.SourceURL, img.SourceURL)
	}
	if got.FilePath != img.FilePath {
		t.Errorf("FilePath: got %q, want %q", got.FilePath, img.FilePath)
	}
}

func TestGetImage_NotFound_ReturnsNil(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	got, err := d.GetImage(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown image ID, got %+v", got)
	}
}

func TestDeleteImage_OwnerCanDelete(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertUser(ctx, "owner2", "Owner2", "o2@example.com"); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := d.InsertImage(ctx, Image{ID: "del1", OwnerID: "owner2", SourceURL: "u", FilePath: "del1.png"}); err != nil {
		t.Fatalf("InsertImage: %v", err)
	}

	deleted, err := d.DeleteImage(ctx, "del1", "owner2")
	if err != nil {
		t.Fatalf("DeleteImage: %v", err)
	}
	if !deleted {
		t.Error("expected deleted=true for owner deleting own image")
	}

	// Confirm it's gone.
	got, err := d.GetImage(ctx, "del1")
	if err != nil {
		t.Fatalf("GetImage after delete: %v", err)
	}
	if got != nil {
		t.Error("image should be gone after deletion")
	}
}

func TestDeleteImage_NonOwnerCannotDelete(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertUser(ctx, "owner3", "Owner3", "o3@example.com"); err != nil {
		t.Fatalf("UpsertUser owner3: %v", err)
	}
	if err := d.UpsertUser(ctx, "other3", "Other3", "other3@example.com"); err != nil {
		t.Fatalf("UpsertUser other3: %v", err)
	}
	if err := d.InsertImage(ctx, Image{ID: "del2", OwnerID: "owner3", SourceURL: "u", FilePath: "del2.png"}); err != nil {
		t.Fatalf("InsertImage: %v", err)
	}

	deleted, err := d.DeleteImage(ctx, "del2", "other3")
	if err != nil {
		t.Fatalf("DeleteImage: %v", err)
	}
	if deleted {
		t.Error("expected deleted=false when non-owner attempts deletion")
	}
}

func TestDeleteImage_NotFound_ReturnsFalse(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	deleted, err := d.DeleteImage(ctx, "ghost", "anyuser")
	if err != nil {
		t.Fatalf("DeleteImage: %v", err)
	}
	if deleted {
		t.Error("expected deleted=false for non-existent image")
	}
}

func TestListRecentImages_OrderedNewestFirst(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertUser(ctx, "lister", "Lister", "lister@example.com"); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	// Insert with explicit timestamps so ordering is deterministic regardless
	// of SQLite's second-granularity CURRENT_TIMESTAMP.
	rows := []struct {
		id  string
		ts  string
	}{
		{"img-a", "2024-01-01 10:00:00"},
		{"img-b", "2024-01-01 10:00:01"},
		{"img-c", "2024-01-01 10:00:02"},
	}
	for _, row := range rows {
		_, err := d.sql.ExecContext(ctx,
			`INSERT INTO images (id, owner_id, source_url, file_path, created_at) VALUES (?, ?, ?, ?, ?)`,
			row.id, "lister", "u", row.id+".png", row.ts)
		if err != nil {
			t.Fatalf("insert image %q: %v", row.id, err)
		}
	}

	imgs, err := d.ListRecentImages(ctx, "lister", 10)
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
}

func TestListRecentImages_LimitRespected(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertUser(ctx, "limiter", "Limiter", "limiter@example.com"); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		if err := d.InsertImage(ctx, Image{ID: id, OwnerID: "limiter", SourceURL: "u", FilePath: id + ".png"}); err != nil {
			t.Fatalf("InsertImage %q: %v", id, err)
		}
	}

	imgs, err := d.ListRecentImages(ctx, "limiter", 3)
	if err != nil {
		t.Fatalf("ListRecentImages: %v", err)
	}
	if len(imgs) != 3 {
		t.Errorf("expected 3 images with limit=3, got %d", len(imgs))
	}
}

func TestListRecentImages_OtherUsersNotReturned(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertUser(ctx, "user-x", "X", "x@example.com"); err != nil {
		t.Fatalf("UpsertUser user-x: %v", err)
	}
	if err := d.UpsertUser(ctx, "user-y", "Y", "y@example.com"); err != nil {
		t.Fatalf("UpsertUser user-y: %v", err)
	}
	if err := d.InsertImage(ctx, Image{ID: "ximg", OwnerID: "user-x", SourceURL: "u", FilePath: "ximg.png"}); err != nil {
		t.Fatalf("InsertImage: %v", err)
	}

	imgs, err := d.ListRecentImages(ctx, "user-y", 10)
	if err != nil {
		t.Fatalf("ListRecentImages: %v", err)
	}
	if len(imgs) != 0 {
		t.Errorf("expected 0 images for user-y, got %d", len(imgs))
	}
}
