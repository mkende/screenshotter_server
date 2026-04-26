package db

import (
	"context"
	"strings"
	"testing"
)

// insertUserAndImages is a helper that inserts a user and a fixed number of
// images for them, returning the generated image IDs.
func insertUserAndImages(t *testing.T, d *DB, email, displayName string, imageCount int) []string {
	t.Helper()
	ctx := context.Background()
	if err := d.UpsertUser(ctx, email, displayName, ""); err != nil {
		t.Fatalf("UpsertUser %q: %v", email, err)
	}
	ids := make([]string, imageCount)
	for i := range ids {
		ids[i] = email + "-img" + string(rune('a'+i))
		if err := d.InsertImage(ctx, Image{
			ID:       ids[i],
			OwnerID:  email,
			FilePath: ids[i] + ".png",
		}); err != nil {
			t.Fatalf("InsertImage %q: %v", ids[i], err)
		}
	}
	return ids
}

// --- ListUsers tests ---------------------------------------------------------

func TestListUsers_ReturnsAllUsersWithImageCounts(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			insertUserAndImages(t, b.db, "lu-alice@x.com", "Alice", 3)
			insertUserAndImages(t, b.db, "lu-bob@x.com", "Bob", 0)

			users, total, err := b.db.ListUsers(ctx, "", 100, 0)
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			if total < 2 {
				t.Fatalf("total should be at least 2, got %d", total)
			}

			byEmail := make(map[string]UserWithStats, len(users))
			for _, u := range users {
				byEmail[u.Email] = u
			}

			if alice, ok := byEmail["lu-alice@x.com"]; !ok {
				t.Error("lu-alice@x.com not in results")
			} else if alice.ImageCount != 3 {
				t.Errorf("alice image count: got %d, want 3", alice.ImageCount)
			}

			if bob, ok := byEmail["lu-bob@x.com"]; !ok {
				t.Error("lu-bob@x.com not in results")
			} else if bob.ImageCount != 0 {
				t.Errorf("bob image count: got %d, want 0", bob.ImageCount)
			}
		})
	}
}

func TestListUsers_SearchByEmail(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			if err := b.db.UpsertUser(ctx, "srch-email-yes@x.com", "Searchable", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			if err := b.db.UpsertUser(ctx, "srch-email-no@y.com", "Other", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			users, total, err := b.db.ListUsers(ctx, "srch-email-yes", 100, 0)
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			if total != 1 {
				t.Fatalf("total: got %d, want 1", total)
			}
			if len(users) != 1 || users[0].Email != "srch-email-yes@x.com" {
				t.Errorf("unexpected results: %+v", users)
			}
		})
	}
}

func TestListUsers_SearchByDisplayName(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			if err := b.db.UpsertUser(ctx, "srch-name-a@x.com", "UniqueDisplayAlpha", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			if err := b.db.UpsertUser(ctx, "srch-name-b@x.com", "UniqueDisplayBeta", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			users, total, err := b.db.ListUsers(ctx, "UniqueDisplayAlpha", 100, 0)
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			if total != 1 {
				t.Fatalf("total: got %d, want 1", total)
			}
			if len(users) != 1 || users[0].Email != "srch-name-a@x.com" {
				t.Errorf("unexpected results: %+v", users)
			}
		})
	}
}

func TestListUsers_SearchIsCaseInsensitive(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			if err := b.db.UpsertUser(ctx, "srch-case@UPPER.com", "CaseTest", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			users, total, err := b.db.ListUsers(ctx, "SRCH-CASE@upper.com", 100, 0)
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			if total != 1 || len(users) != 1 {
				t.Errorf("case-insensitive search failed: total=%d, len=%d", total, len(users))
			}
		})
	}
}

func TestListUsers_Pagination(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			// Insert users with a unique prefix to isolate from other tests.
			for i := 0; i < 5; i++ {
				email := "pg-user-" + string(rune('a'+i)) + "@x.com"
				if err := b.db.UpsertUser(ctx, email, "PgUser", ""); err != nil {
					t.Fatalf("UpsertUser %q: %v", email, err)
				}
			}

			// Search for this batch using a prefix that only matches these users.
			_, total, err := b.db.ListUsers(ctx, "pg-user-", 100, 0)
			if err != nil {
				t.Fatalf("ListUsers (total): %v", err)
			}
			if total != 5 {
				t.Fatalf("total: got %d, want 5", total)
			}

			page1, _, err := b.db.ListUsers(ctx, "pg-user-", 3, 0)
			if err != nil {
				t.Fatalf("ListUsers page1: %v", err)
			}
			if len(page1) != 3 {
				t.Errorf("page1 len: got %d, want 3", len(page1))
			}

			page2, _, err := b.db.ListUsers(ctx, "pg-user-", 3, 3)
			if err != nil {
				t.Fatalf("ListUsers page2: %v", err)
			}
			if len(page2) != 2 {
				t.Errorf("page2 len: got %d, want 2", len(page2))
			}

			// Verify no overlap between pages.
			seen := make(map[string]bool)
			for _, u := range append(page1, page2...) {
				if seen[u.Email] {
					t.Errorf("duplicate user %q across pages", u.Email)
				}
				seen[u.Email] = true
			}
		})
	}
}

// --- GetUserWithStats tests --------------------------------------------------

func TestGetUserWithStats_ReturnsImageCount(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			insertUserAndImages(t, b.db, "gus-alice@x.com", "Alice", 4)

			u, err := b.db.GetUserWithStats(context.Background(), "gus-alice@x.com")
			if err != nil {
				t.Fatalf("GetUserWithStats: %v", err)
			}
			if u == nil {
				t.Fatal("expected user, got nil")
			}
			if u.Email != "gus-alice@x.com" {
				t.Errorf("Email: got %q", u.Email)
			}
			if u.ImageCount != 4 {
				t.Errorf("ImageCount: got %d, want 4", u.ImageCount)
			}
		})
	}
}

func TestGetUserWithStats_NoImages_ReturnsZeroCount(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			if err := b.db.UpsertUser(ctx, "gus-empty@x.com", "Empty", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			u, err := b.db.GetUserWithStats(ctx, "gus-empty@x.com")
			if err != nil {
				t.Fatalf("GetUserWithStats: %v", err)
			}
			if u == nil {
				t.Fatal("expected user, got nil")
			}
			if u.ImageCount != 0 {
				t.Errorf("ImageCount: got %d, want 0", u.ImageCount)
			}
		})
	}
}

func TestGetUserWithStats_NotFound_ReturnsNil(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			u, err := b.db.GetUserWithStats(context.Background(), "gus-nobody@x.com")
			if err != nil {
				t.Fatalf("GetUserWithStats: %v", err)
			}
			if u != nil {
				t.Errorf("expected nil, got %+v", u)
			}
		})
	}
}

// --- ReassignImage tests -----------------------------------------------------

func TestReassignImage_ChangesOwner(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			insertUserAndImages(t, b.db, "ri-from@x.com", "From", 1)
			if err := b.db.UpsertUser(ctx, "ri-to@x.com", "To", ""); err != nil {
				t.Fatalf("UpsertUser to: %v", err)
			}
			imageID := "ri-from@x.com-imga"

			ok, err := b.db.ReassignImage(ctx, imageID, "ri-to@x.com")
			if err != nil {
				t.Fatalf("ReassignImage: %v", err)
			}
			if !ok {
				t.Fatal("expected ok=true")
			}

			img, err := b.db.GetImage(ctx, imageID)
			if err != nil {
				t.Fatalf("GetImage: %v", err)
			}
			if img.OwnerID != "ri-to@x.com" {
				t.Errorf("OwnerID after reassign: got %q, want %q", img.OwnerID, "ri-to@x.com")
			}
		})
	}
}

func TestReassignImage_NotFound_ReturnsFalse(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			if err := b.db.UpsertUser(ctx, "ri-nf-to@x.com", "To", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			ok, err := b.db.ReassignImage(ctx, "ri-ghost-img", "ri-nf-to@x.com")
			if err != nil {
				t.Fatalf("ReassignImage: %v", err)
			}
			if ok {
				t.Error("expected ok=false for non-existent image")
			}
		})
	}
}

// --- ReassignAllImages tests -------------------------------------------------

func TestReassignAllImages_MovesAll(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			ids := insertUserAndImages(t, b.db, "raa-from@x.com", "From", 3)
			if err := b.db.UpsertUser(ctx, "raa-to@x.com", "To", ""); err != nil {
				t.Fatalf("UpsertUser to: %v", err)
			}

			n, err := b.db.ReassignAllImages(ctx, "raa-from@x.com", "raa-to@x.com")
			if err != nil {
				t.Fatalf("ReassignAllImages: %v", err)
			}
			if n != 3 {
				t.Errorf("moved count: got %d, want 3", n)
			}

			for _, id := range ids {
				img, err := b.db.GetImage(ctx, id)
				if err != nil {
					t.Fatalf("GetImage %q: %v", id, err)
				}
				if img.OwnerID != "raa-to@x.com" {
					t.Errorf("image %q OwnerID: got %q, want raa-to@x.com", id, img.OwnerID)
				}
			}

			remaining, err := b.db.ListRecentImages(ctx, "raa-from@x.com", 100, 0)
			if err != nil {
				t.Fatalf("ListRecentImages after reassign: %v", err)
			}
			if len(remaining) != 0 {
				t.Errorf("source user should have 0 images after reassign, got %d", len(remaining))
			}
		})
	}
}

func TestReassignAllImages_NoImages_ReturnsZero(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			if err := b.db.UpsertUser(ctx, "raa-z-from@x.com", "EmptyFrom", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			if err := b.db.UpsertUser(ctx, "raa-z-to@x.com", "To", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			n, err := b.db.ReassignAllImages(ctx, "raa-z-from@x.com", "raa-z-to@x.com")
			if err != nil {
				t.Fatalf("ReassignAllImages: %v", err)
			}
			if n != 0 {
				t.Errorf("expected 0 moved, got %d", n)
			}
		})
	}
}

// --- AdminDeleteImage tests --------------------------------------------------

func TestAdminDeleteImage_DeletesRegardlessOfOwner(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			ids := insertUserAndImages(t, b.db, "adi-owner@x.com", "Owner", 1)

			// Delete as a different user — the DB method does not check ownership.
			ok, err := b.db.AdminDeleteImage(ctx, ids[0])
			if err != nil {
				t.Fatalf("AdminDeleteImage: %v", err)
			}
			if !ok {
				t.Fatal("expected ok=true")
			}

			img, err := b.db.GetImage(ctx, ids[0])
			if err != nil {
				t.Fatalf("GetImage: %v", err)
			}
			if img != nil {
				t.Error("image should be gone from DB")
			}
		})
	}
}

func TestAdminDeleteImage_NotFound_ReturnsFalse(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ok, err := b.db.AdminDeleteImage(context.Background(), "adi-ghost")
			if err != nil {
				t.Fatalf("AdminDeleteImage: %v", err)
			}
			if ok {
				t.Error("expected ok=false for non-existent image")
			}
		})
	}
}

// --- ListImageIDsByOwner tests -----------------------------------------------

func TestListImageIDsByOwner_ReturnsAllIDs(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			ids := insertUserAndImages(t, b.db, "libo-owner@x.com", "Owner", 4)

			got, err := b.db.ListImageIDsByOwner(ctx, "libo-owner@x.com")
			if err != nil {
				t.Fatalf("ListImageIDsByOwner: %v", err)
			}
			if len(got) != len(ids) {
				t.Fatalf("got %d IDs, want %d", len(got), len(ids))
			}

			want := make(map[string]bool, len(ids))
			for _, id := range ids {
				want[id] = true
			}
			for _, id := range got {
				if !want[id] {
					t.Errorf("unexpected ID %q in results", id)
				}
			}
		})
	}
}

func TestListImageIDsByOwner_Empty_ReturnsNilSlice(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			if err := b.db.UpsertUser(ctx, "libo-empty@x.com", "Empty", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			ids, err := b.db.ListImageIDsByOwner(ctx, "libo-empty@x.com")
			if err != nil {
				t.Fatalf("ListImageIDsByOwner: %v", err)
			}
			if len(ids) != 0 {
				t.Errorf("expected empty result, got %v", ids)
			}
		})
	}
}

// --- DeleteUser tests --------------------------------------------------------

func TestDeleteUser_CascadesImages(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			ids := insertUserAndImages(t, b.db, "du-owner@x.com", "Owner", 3)

			ok, err := b.db.DeleteUser(ctx, "du-owner@x.com")
			if err != nil {
				t.Fatalf("DeleteUser: %v", err)
			}
			if !ok {
				t.Fatal("expected ok=true")
			}

			// User should be gone.
			u, err := b.db.GetUser(ctx, "du-owner@x.com")
			if err != nil {
				t.Fatalf("GetUser: %v", err)
			}
			if u != nil {
				t.Error("user should be deleted")
			}

			// All images should be cascade-deleted.
			for _, id := range ids {
				img, err := b.db.GetImage(ctx, id)
				if err != nil {
					t.Fatalf("GetImage %q: %v", id, err)
				}
				if img != nil {
					t.Errorf("image %q should be cascade-deleted with the user", id)
				}
			}
		})
	}
}

func TestDeleteUser_NotFound_ReturnsFalse(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ok, err := b.db.DeleteUser(context.Background(), "du-nobody@x.com")
			if err != nil {
				t.Fatalf("DeleteUser: %v", err)
			}
			if ok {
				t.Error("expected ok=false for non-existent user")
			}
		})
	}
}

// --- ListUsers wildcard escape test ------------------------------------------

func TestListUsers_SearchEscapesWildcards(t *testing.T) {
	for _, b := range allBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			// These two users differ only in that one has a literal '%' in the
			// display name; the other should not be matched.
			if err := b.db.UpsertUser(ctx, "wc-pct@x.com", "Name%With%Percent", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			if err := b.db.UpsertUser(ctx, "wc-plain@x.com", "NameWithPercent", ""); err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}

			// Searching for the literal '%' should only match the first user.
			users, _, err := b.db.ListUsers(ctx, "Name%With%Percent", 100, 0)
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			for _, u := range users {
				if u.Email == "wc-plain@x.com" {
					// '%' in the search term should not have acted as a SQL wildcard
					// and matched the plain-name user.
					t.Errorf("wildcard escape failed: 'Name%%With%%Percent' matched plain user %q", u.Email)
				}
			}
			if !func() bool {
				for _, u := range users {
					if u.Email == "wc-pct@x.com" && strings.Contains(u.DisplayName, "%") {
						return true
					}
				}
				return false
			}() {
				t.Error("expected to find the user whose display name contains literal '%'")
			}
		})
	}
}
