package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

func openTest(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background(), migrations.FS); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMigrateIsIdempotent(t *testing.T) {
	st := openTest(t)
	if err := st.Migrate(context.Background(), migrations.FS); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMigrateRejectsBadNames(t *testing.T) {
	st := openTest(t)
	bad := fstest.MapFS{"init.sql": {Data: []byte("SELECT 1")}}
	if err := st.Migrate(context.Background(), bad); err == nil {
		t.Fatal("expected error for migration without version prefix")
	}
}

func TestUniqueConstraintsMapToConflict(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Now()

	u := &store.User{Username: "alice", PasswordHash: "x", Locale: "uk", CreatedAt: now}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	dup := &store.User{Username: "ALICE", PasswordHash: "x", Locale: "uk", CreatedAt: now}
	if err := st.CreateUser(ctx, dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate username (case-insensitive): got %v, want ErrConflict", err)
	}

	g := &store.Group{Code: "KIUKI-25-3", Name: "KIUKI-25-3", InviteCode: "AAAA-BBBB-CCCC", CreatedAt: now}
	if err := st.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	g2 := &store.Group{Code: "OTHER", Name: "OTHER", InviteCode: "AAAA-BBBB-CCCC", CreatedAt: now}
	if err := st.CreateGroup(ctx, g2); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate invite code: got %v, want ErrConflict", err)
	}

	if _, err := st.UserByUsername(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing user: got %v, want ErrNotFound", err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Now()
	u := &store.User{Username: "bob", PasswordHash: "x", Locale: "uk", CreatedAt: now}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, &store.Session{ID: "s1", UserID: u.ID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// Foreign keys must be on, otherwise this would leave an orphan session.
	if err := st.CreateSession(ctx, &store.Session{ID: "s2", UserID: 9999, ExpiresAt: now, CreatedAt: now}); err == nil {
		t.Fatal("expected foreign key violation for unknown user")
	}
}
