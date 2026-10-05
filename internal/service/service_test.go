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
	svc    *service.Service
	invite string
	group  *store.Group
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
	return &fixture{svc: svc, invite: g.InviteCode, group: g}
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
		Username: username, Password: "correct horse", InviteCode: f.invite, ClientIP: "ip-" + username,
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
	if len(groups) != 1 || groups[0].InviteCode != "" || groups[0].CanManage {
		t.Errorf("student must not see the invite code: %+v", groups)
	}
}

func TestRegisterValidation(t *testing.T) {
	f := setup(t)
	f.register(t, "taken")
	cases := []struct {
		name string
		in   service.RegisterInput
		want error
	}{
		{"bad invite", service.RegisterInput{Username: "bob", Password: "correct horse", InviteCode: "ZZZZ-ZZZZ-ZZZZ"}, service.ErrInvalidInviteCode},
		{"taken username", service.RegisterInput{Username: "TAKEN", Password: "correct horse", InviteCode: f.invite}, service.ErrUsernameTaken},
	}
	for _, c := range cases {
		c.in.ClientIP = c.name
		if _, err := f.svc.Register(context.Background(), c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}

	inputErrs := []service.RegisterInput{
		{Username: "ab", Password: "correct horse", InviteCode: f.invite},
		{Username: "_bob", Password: "correct horse", InviteCode: f.invite},
		{Username: "боб", Password: "correct horse", InviteCode: f.invite},
		{Username: "bob", Password: "short", InviteCode: f.invite},
		{Username: "bob", Password: "correct horse", InviteCode: ""},
	}
	for _, in := range inputErrs {
		in.ClientIP = in.Username + in.Password
		var ie *service.InputError
		if _, err := f.svc.Register(context.Background(), in); !errors.As(err, &ie) {
			t.Errorf("%+v: got %v, want InputError", in, err)
		}
	}
}

func TestInviteCodeIsForgiving(t *testing.T) {
	f := setup(t)
	sloppy := "  " + f.invite[:4] + f.invite[5:9] + " " + f.invite[10:] + " "
	_, err := f.svc.Register(context.Background(), service.RegisterInput{
		Username: "carol", Password: "correct horse", InviteCode: sloppy, ClientIP: "x",
	})
	if err != nil {
		t.Fatalf("register with %q: %v", sloppy, err)
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

func TestRegenerateInviteCodePermissions(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	student := f.as(t, f.register(t, "frank"))
	leaderSess := f.register(t, "grace")

	if _, err := f.svc.RegenerateInviteCode(student, f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student: got %v, want ErrForbidden", err)
	}
	if _, err := f.svc.RegenerateInviteCode(ctx, f.group.ID); !errors.Is(err, service.ErrUnauthenticated) {
		t.Fatalf("anonymous: got %v, want ErrUnauthenticated", err)
	}

	if err := f.svc.AdminSetRole(ctx, "grace", "KIUKI-25-3", store.RoleLeader); err != nil {
		t.Fatal(err)
	}
	leader := f.as(t, leaderSess) // memberships are loaded per request
	groups, err := f.svc.MyGroups(leader)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].InviteCode != f.invite || !groups[0].CanManage {
		t.Fatalf("leader should see the invite code: %+v", groups)
	}

	code, err := f.svc.RegenerateInviteCode(leader, f.group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if code == f.invite {
		t.Fatal("invite code did not change")
	}
	_, err = f.svc.Register(ctx, service.RegisterInput{Username: "heidi", Password: "correct horse", InviteCode: f.invite, ClientIP: "h"})
	if !errors.Is(err, service.ErrInvalidInviteCode) {
		t.Fatalf("old invite code: got %v, want ErrInvalidInviteCode", err)
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
	if len(groups) != 2 || !groups[0].CanManage || groups[0].InviteCode == "" {
		t.Fatalf("superadmin groups: %+v", groups)
	}
	if _, err := f.svc.RegenerateInviteCode(admin, f.group.ID); err != nil {
		t.Fatalf("superadmin regenerate: %v", err)
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
