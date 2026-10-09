package store_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

func openTest(t *testing.T) *store.Store {
	t.Helper()
	return openTestWith(t, migrations.FS)
}

// openTestWith opens a fresh database migrated with the migrations in fsys.
func openTestWith(t *testing.T, fsys fs.FS) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background(), fsys); err != nil {
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

	g := &store.Group{Code: "KIUKI-25-3", Name: "KIUKI-25-3", CreatedAt: now}
	if err := st.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	g2 := &store.Group{Code: "kiuki-25-3", Name: "Other", CreatedAt: now}
	if err := st.CreateGroup(ctx, g2); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate group code (case-insensitive): got %v, want ErrConflict", err)
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

// The 0002 migration rebuilds the groups table; memberships that point at it
// must survive.
func TestOpenGroupsMigrationKeepsMemberships(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	initSQL, err := fs.ReadFile(migrations.FS, "0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx, fstest.MapFS{"0001_init.sql": {Data: initSQL}}); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `
		INSERT INTO groups (id, code, name, cist_group_id, invite_code, created_at) VALUES (7, 'KIUKI-25-3', 'KIUKI-25-3', 42, 'AAAA-BBBB-CCCC', 1);
		INSERT INTO users (id, username, password_hash, created_at) VALUES (3, 'alice', 'x', 1);
		INSERT INTO memberships (user_id, group_id, role, joined_at) VALUES (3, 7, 'leader', 1);`); err != nil {
		t.Fatal(err)
	}

	if err := st.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	g, err := st.GroupByCode(ctx, "kiuki-25-3")
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != 7 || g.CISTGroupID == nil || *g.CISTGroupID != 42 {
		t.Fatalf("group after migration: %+v", g)
	}
	ms, err := st.MembershipsByUser(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].GroupID != 7 || ms[0].Role != store.RoleLeader {
		t.Fatalf("memberships after migration: %+v", ms)
	}
	// Foreign keys are back on for normal queries.
	if err := st.AddMembership(ctx, &store.Membership{UserID: 3, GroupID: 999, Role: store.RoleStudent, JoinedAt: time.Now()}); err == nil {
		t.Fatal("expected foreign key violation for unknown group")
	}
}

func TestMigrateRejectsForeignKeyViolations(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	bad := fstest.MapFS{"9999_orphan.sql": {Data: []byte(
		`INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES ('s', 12345, 0, 0)`)}}
	if err := st.Migrate(ctx, bad); err == nil {
		t.Fatal("expected migration leaving an orphan row to fail")
	}
	if _, err := st.SessionByID(ctx, "s"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed migration was not rolled back: %v", err)
	}
}

func TestSubjectNamesUniqueIgnoringCase(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	g := &store.Group{Code: "G", Name: "G", CreatedAt: time.Now()}
	if err := st.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSubject(ctx, &store.Subject{GroupID: g.ID, Name: "Фізика"}); err != nil {
		t.Fatal(err)
	}
	other := &store.Subject{GroupID: g.ID, Name: "Хімія"}
	if err := st.CreateSubject(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSubject(ctx, &store.Subject{GroupID: g.ID, Name: "ФІЗИКА"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("create: got %v, want ErrConflict", err)
	}
	other.Name = "фізика"
	if err := st.UpdateSubject(ctx, other); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("rename: got %v, want ErrConflict", err)
	}
}

func TestBackup(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := &store.User{Username: "alice", PasswordHash: "x", CreatedAt: time.Now()}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backup.db")
	if err := st.Backup(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := st.Backup(ctx, path); err == nil {
		t.Fatal("backup over an existing file should fail")
	}
	cp, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	if got, err := cp.UserByUsername(ctx, "alice"); err != nil || got.ID != u.ID {
		t.Fatalf("user in backup: %v %v", got, err)
	}
}

// The 0010 migration gives existing groups an invite link and keeps only the
// first leader of a group that has several.
func TestInvitesMigration(t *testing.T) {
	ctx := context.Background()
	before := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		// 0013 only adds a users column, which CreateUser below writes.
		if name >= "0010" && name != "0013_user_time_zone.sql" {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: data}
	}
	st := openTestWith(t, before)

	g := &store.Group{Code: "G", Name: "G", CreatedAt: time.Now()}
	if err := st.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"first", "second", "third"} {
		u := &store.User{Username: name, PasswordHash: "x", CreatedAt: time.Now()}
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		role := store.RoleLeader
		if name == "third" {
			role = store.RoleStudent
		}
		if err := st.AddMembership(ctx, &store.Membership{UserID: u.ID, GroupID: g.ID, Role: role, JoinedAt: time.Unix(int64(100+i), 0)}); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	inv, err := st.InviteByGroup(ctx, g.ID)
	if err != nil || len(inv.Token) != 64 || !inv.ExpiresAt.After(time.Now().Add(9*24*time.Hour)) {
		t.Fatalf("invite after migration: %+v %v", inv, err)
	}
	if lead, err := st.LeaderOf(ctx, g.ID); err != nil || lead.Username != "first" {
		t.Fatalf("leader after migration: %+v %v", lead, err)
	}
	members, err := st.ListMembers(ctx, g.ID)
	if err != nil || len(members) != 3 {
		t.Fatalf("members after migration: %v %v", members, err)
	}
	// The database refuses a second leader from now on.
	if err := st.SetRole(ctx, members[1].UserID, g.ID, store.RoleLeader); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second leader: got %v, want ErrConflict", err)
	}
}
