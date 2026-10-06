package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

func TestMain(m *testing.M) {
	auth.BcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

type fixture struct {
	svc   *service.Service
	group *store.Group
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	svc := service.New(st, service.Config{})
	g, err := svc.AdminCreateGroup(ctx, "kiuki-25-3", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{svc: svc, group: g}
}

// as returns a context signed in as the session's user.
func (f *fixture) as(t *testing.T, sess *service.NewSession) context.Context {
	t.Helper()
	ctx, err := f.svc.Authenticate(context.Background(), sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func (f *fixture) register(t *testing.T, username string) *service.NewSession {
	t.Helper()
	sess, err := f.svc.Register(context.Background(), service.RegisterInput{
		Username: username, Password: "correct horse", GroupCode: f.group.Code, ClientIP: "ip-" + username,
	})
	if err != nil {
		t.Fatalf("register %s: %v", username, err)
	}
	return sess
}

func TestCreateGroupNormalizesCode(t *testing.T) {
	f := setup(t)
	if f.group.Code != "KIUKI-25-3" || f.group.Name != "KIUKI-25-3" {
		t.Fatalf("got code=%q name=%q", f.group.Code, f.group.Name)
	}
	if _, err := f.svc.AdminCreateGroup(context.Background(), "KIUKI-25-3", "", nil); err == nil {
		t.Fatal("expected duplicate group error")
	}
}

func TestRegisterJoinsGroupAsStudent(t *testing.T) {
	f := setup(t)
	ctx := f.as(t, f.register(t, "Alice"))
	v := service.ViewerFrom(ctx)
	if v.Username != "alice" {
		t.Errorf("username = %q, want lowercased", v.Username)
	}
	if role, ok := v.RoleIn(f.group.ID); !ok || role != store.RoleStudent {
		t.Errorf("role = %q, %v; want student", role, ok)
	}

	groups, err := f.svc.MyGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Code != "KIUKI-25-3" || groups[0].CanManage {
		t.Errorf("student groups: %+v", groups)
	}
}

func TestRegisterValidation(t *testing.T) {
	f := setup(t)
	f.register(t, "taken")
	_, err := f.svc.Register(context.Background(), service.RegisterInput{
		Username: "TAKEN", Password: "correct horse", GroupCode: f.group.Code, ClientIP: "t",
	})
	if !errors.Is(err, service.ErrUsernameTaken) {
		t.Errorf("taken username: got %v, want ErrUsernameTaken", err)
	}

	inputErrs := []service.RegisterInput{
		{Username: "ab", Password: "correct horse", GroupCode: f.group.Code},
		{Username: "_bob", Password: "correct horse", GroupCode: f.group.Code},
		{Username: "боб", Password: "correct horse", GroupCode: f.group.Code},
		{Username: "bob", Password: "short", GroupCode: f.group.Code},
		{Username: "bob", Password: "correct horse", GroupCode: ""},
		{Username: "bob", Password: "correct horse", GroupCode: "NO-SUCH-GROUP"},
	}
	for _, in := range inputErrs {
		in.ClientIP = in.Username + in.Password + in.GroupCode
		var ie *service.InputError
		if _, err := f.svc.Register(context.Background(), in); !errors.As(err, &ie) {
			t.Errorf("%+v: got %v, want InputError", in, err)
		}
	}
}

func TestAnyoneCanJoinAnyGroup(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	other, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "Other group", nil)
	if err != nil {
		t.Fatal(err)
	}

	groups, err := f.svc.JoinableGroups(ctx) // no viewer needed
	if err != nil {
		t.Fatal(err)
	}
	want := []service.JoinableGroup{{Code: "KIUKI-25-3", Name: "KIUKI-25-3"}, {Code: "OTHER-1", Name: "Other group"}}
	if len(groups) != len(want) || groups[0] != want[0] || groups[1] != want[1] {
		t.Fatalf("joinable groups = %+v, want %+v", groups, want)
	}

	sess, err := f.svc.Register(ctx, service.RegisterInput{
		Username: "olga", Password: "correct horse", GroupCode: " other-1 ", ClientIP: "o",
	})
	if err != nil {
		t.Fatal(err)
	}
	v := service.ViewerFrom(f.as(t, sess))
	if role, ok := v.RoleIn(other.ID); !ok || role != store.RoleStudent {
		t.Errorf("role in OTHER-1 = %q, %v; want student", role, ok)
	}
	if _, ok := v.RoleIn(f.group.ID); ok {
		t.Error("should not be a member of a group they did not pick")
	}
}

func TestLogin(t *testing.T) {
	f := setup(t)
	f.register(t, "dave")
	ctx := context.Background()

	if _, err := f.svc.Login(ctx, service.LoginInput{Username: "dave", Password: "wrong password", ClientIP: "1"}); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("wrong password: got %v", err)
	}
	if _, err := f.svc.Login(ctx, service.LoginInput{Username: "nobody", Password: "whatever1", ClientIP: "1"}); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("unknown user: got %v", err)
	}
	sess, err := f.svc.Login(ctx, service.LoginInput{Username: " Dave ", Password: "correct horse", ClientIP: "1"})
	if err != nil {
		t.Fatal(err)
	}
	f.as(t, sess)

	if err := f.svc.Logout(ctx, sess.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("after logout: got %v, want ErrNoSession", err)
	}
}

func TestLoginRateLimitPerUsername(t *testing.T) {
	f := setup(t)
	f.register(t, "erin")
	ctx := context.Background()
	var err error
	for i := range 11 {
		_, err = f.svc.Login(ctx, service.LoginInput{Username: "erin", Password: "nope-nope", ClientIP: string(rune('a' + i))})
	}
	if !errors.Is(err, service.ErrRateLimited) {
		t.Fatalf("11th attempt: got %v, want ErrRateLimited", err)
	}
	// Even the right password is refused while limited.
	if _, err := f.svc.Login(ctx, service.LoginInput{Username: "erin", Password: "correct horse", ClientIP: "z"}); !errors.Is(err, service.ErrRateLimited) {
		t.Fatalf("got %v, want ErrRateLimited", err)
	}
}

func TestOnlyAdminCLIMakesLeaders(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sess := f.register(t, "grace")
	if v := service.ViewerFrom(f.as(t, sess)); v.CanManageGroup(f.group.ID) {
		t.Fatal("a freshly registered user must not manage the group")
	}

	if err := f.svc.AdminSetRole(ctx, "grace", "kiuki-25-3", store.RoleLeader); err != nil {
		t.Fatal(err)
	}
	leader := f.as(t, sess) // memberships are loaded per request
	if role, _ := service.ViewerFrom(leader).RoleIn(f.group.ID); role != store.RoleLeader {
		t.Fatalf("role after promote = %q", role)
	}
	groups, err := f.svc.MyGroups(leader)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || !groups[0].CanManage {
		t.Fatalf("leader groups: %+v", groups)
	}

	if err := f.svc.AdminSetRole(ctx, "grace", "KIUKI-25-3", store.RoleStudent); err != nil {
		t.Fatal(err)
	}
	if service.ViewerFrom(f.as(t, sess)).CanManageGroup(f.group.ID) {
		t.Fatal("demoted user still manages the group")
	}
	if err := f.svc.AdminSetRole(ctx, "nobody", "KIUKI-25-3", store.RoleLeader); err == nil {
		t.Fatal("promoting an unknown user should fail")
	}
}

func TestSuperadminSeesAllGroups(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.svc.AdminCreateSuperadmin(ctx, "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "Other", nil); err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Login(ctx, service.LoginInput{Username: "root", Password: "correct horse", ClientIP: "r"})
	if err != nil {
		t.Fatal(err)
	}
	admin := f.as(t, sess)
	groups, err := f.svc.MyGroups(admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || !groups[0].CanManage || !groups[1].CanManage || groups[0].Role != "" {
		t.Fatalf("superadmin groups: %+v", groups)
	}
}

func TestAdminResetPasswordRevokesSessions(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sess := f.register(t, "ivan")
	if err := f.svc.AdminResetPassword(ctx, "ivan", "brand new pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("old session still valid: %v", err)
	}
	if _, err := f.svc.Login(ctx, service.LoginInput{Username: "ivan", Password: "brand new pass", ClientIP: "i"}); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
}
