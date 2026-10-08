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
	return f.registerInto(t, f.group.Code, username)
}

// invite returns the token of a group's invite link.
func (f *fixture) invite(t *testing.T, groupCode string) string {
	t.Helper()
	inv, err := f.svc.AdminInvite(context.Background(), groupCode, false)
	if err != nil {
		t.Fatal(err)
	}
	return inv.Token
}

// registerInto registers username through the invite link of a group.
func (f *fixture) registerInto(t *testing.T, groupCode, username string) *service.NewSession {
	t.Helper()
	sess, err := f.svc.Register(context.Background(), service.RegisterInput{
		Username: username, Password: "correct horse", InviteToken: f.invite(t, groupCode), ClientIP: "ip-" + username,
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
	token := f.invite(t, f.group.Code)
	_, err := f.svc.Register(context.Background(), service.RegisterInput{
		Username: "TAKEN", Password: "correct horse", InviteToken: token, ClientIP: "t",
	})
	if !errors.Is(err, service.ErrUsernameTaken) {
		t.Errorf("taken username: got %v, want ErrUsernameTaken", err)
	}

	inputErrs := []service.RegisterInput{
		{Username: "ab", Password: "correct horse"},
		{Username: "_bob", Password: "correct horse"},
		{Username: "боб", Password: "correct horse"},
		{Username: "bob", Password: "short"},
	}
	for _, in := range inputErrs {
		in.InviteToken = token
		in.ClientIP = in.Username + in.Password
		var ie *service.InputError
		if _, err := f.svc.Register(context.Background(), in); !errors.As(err, &ie) {
			t.Errorf("%+v: got %v, want InputError", in, err)
		}
	}
	for _, bad := range []string{"", "no-such-token"} {
		_, err := f.svc.Register(context.Background(), service.RegisterInput{
			Username: "bob", Password: "correct horse", InviteToken: bad, ClientIP: "bad" + bad,
		})
		if !errors.Is(err, service.ErrInviteInvalid) {
			t.Errorf("invite %q: got %v, want ErrInviteInvalid", bad, err)
		}
	}
}

func TestRegisterJoinsTheInvitesGroup(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	other, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "Other group", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := service.ViewerFrom(f.as(t, f.registerInto(t, "OTHER-1", "olga")))
	if role, ok := v.RoleIn(other.ID); !ok || role != store.RoleStudent {
		t.Errorf("role in OTHER-1 = %q, %v; want student", role, ok)
	}
	if _, ok := v.RoleIn(f.group.ID); ok {
		t.Error("should not be a member of a group whose link they did not use")
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

func TestAdminCLISetsTheLeader(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sess := f.register(t, "grace")
	if v := service.ViewerFrom(f.as(t, sess)); v.CanManageGroup(f.group.ID) {
		t.Fatal("a freshly registered user must not manage the group")
	}

	if err := f.svc.AdminSetLeader(ctx, "grace", "kiuki-25-3"); err != nil {
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

	// Promoting someone else replaces the leader.
	hank := f.register(t, "hank")
	if err := f.svc.AdminSetLeader(ctx, "hank", "KIUKI-25-3"); err != nil {
		t.Fatal(err)
	}
	if service.ViewerFrom(f.as(t, sess)).CanManageGroup(f.group.ID) {
		t.Fatal("the previous leader still manages the group")
	}
	if !service.ViewerFrom(f.as(t, hank)).CanManageGroup(f.group.ID) {
		t.Fatal("the new leader does not manage the group")
	}

	if err := f.svc.AdminRemoveLeader(ctx, "grace", "KIUKI-25-3"); err == nil {
		t.Fatal("demoting someone who is not the leader should fail")
	}
	if err := f.svc.AdminRemoveLeader(ctx, "hank", "KIUKI-25-3"); err != nil {
		t.Fatal(err)
	}
	if service.ViewerFrom(f.as(t, hank)).CanManageGroup(f.group.ID) {
		t.Fatal("demoted user still manages the group")
	}
	if err := f.svc.AdminSetLeader(ctx, "nobody", "KIUKI-25-3"); err == nil {
		t.Fatal("promoting an unknown user should fail")
	}

	// A member of another group cannot be made leader here.
	if _, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AdminSetLeader(ctx, "grace", "OTHER-1"); err == nil {
		t.Fatal("a user can be in one group only")
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
