package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// Sessions are cached, but every change shows on the next request.
func TestCachedSessionFollowsChanges(t *testing.T) {
	f := setup(t)
	sess := f.register(t, "stud")
	viewer := func() *service.Viewer {
		t.Helper()
		return service.ViewerFrom(f.as(t, sess))
	}
	if v := viewer(); v.Locale != "" || len(v.Memberships) != 1 {
		t.Fatalf("new user: %+v", v)
	}

	ctx := f.as(t, sess)
	if err := f.svc.SetLocale(ctx, "en"); err != nil {
		t.Fatal(err)
	}
	if v := viewer(); v.Locale != "en" {
		t.Errorf("locale after SetLocale: %q", v.Locale)
	}

	// A request changing its viewer does not change the cached one.
	service.ViewerFrom(f.as(t, sess)).Locale = "uk"
	if v := viewer(); v.Locale != "en" {
		t.Errorf("a request's change to its viewer leaked into the cache: %q", v.Locale)
	}

	if err := f.svc.LeaveGroup(f.as(t, sess), f.group.ID); err != nil {
		t.Fatal(err)
	}
	if v := viewer(); len(v.Memberships) != 0 {
		t.Errorf("memberships after leaving: %v", v.Memberships)
	}
	if _, err := f.svc.Group(f.as(t, sess), f.group.Code); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("group after leaving: %v", err)
	}

	if err := f.svc.Logout(context.Background(), sess.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(context.Background(), sess.Token); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("after logout: %v", err)
	}
}
