package server_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestDemoPages(t *testing.T) {
	e := newEnvWith(t, service.Config{DemoGroup: "KIUKI-25-3"})
	passwords, err := e.svc.ResetDemo(context.Background(), "KIUKI-25-3")
	if err != nil {
		t.Fatal(err)
	}
	password := passwords[service.DemoUsername]
	b := e.browser(t)
	// The superadmin gives the password out; the site never shows it.
	if _, body, _ := b.get("/login"); strings.Contains(body, password) || strings.Contains(strings.ToLower(body), "demo") {
		t.Fatalf("the login page should not mention the demo account:\n%s", body)
	}
	if code, _, h := b.submit("/login", "/login", url.Values{"username": {"demo"}, "password": {password}}); code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("demo login: %d %s", code, h.Get("Location"))
	}
	if _, body, _ := b.get("/g/KIUKI-25-3/homework"); !strings.Contains(body, "Лабораторна робота №3") {
		t.Fatalf("demo homework:\n%s", body)
	}
	// No buttons for what the demo account may not do, and a clear refusal
	// for a form posted all the same.
	_, body, _ := b.get("/g/KIUKI-25-3/members")
	if strings.Contains(body, "/leader/resign") || strings.Contains(body, "/leave") || strings.Contains(body, "/join/") {
		t.Fatalf("the demo leader should not be offered resigning, leaving or the invite link:\n%s", body)
	}
	// As the leader it can change the group's content.
	if code, _, _ := b.submit("/g/KIUKI-25-3/notes", "/g/KIUKI-25-3/notes", url.Values{"title": {"Hi"}, "body": {"test"}}); code != http.StatusSeeOther {
		t.Fatalf("demo leader adds a note: %d", code)
	}
	if code, body, _ := b.submit("/g/KIUKI-25-3/members", "/g/KIUKI-25-3/leave", url.Values{}); code != http.StatusForbidden || !strings.Contains(body, "not available in the demo group") {
		t.Fatalf("leave: %d\n%s", code, body)
	}
}

// A real account that got the name demo before the site had a demo, in
// another group, is not a demo account: it may leave its group.
func TestRealAccountNamedDemo(t *testing.T) {
	e := newEnvWith(t, service.Config{DemoGroup: "DEMO"})
	ctx := context.Background()
	if _, err := e.svc.AdminCreateGroup(ctx, "DEMO", "", nil); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	u := &store.User{Username: "demo", PasswordHash: hash, CreatedAt: time.Now()}
	if err := e.st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	g, err := e.st.GroupByCode(ctx, "KIUKI-25-3")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.AddMembership(ctx, &store.Membership{UserID: u.ID, GroupID: g.ID, Role: store.RoleStudent, JoinedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	b := e.browser(t)
	b.submit("/login", "/login", url.Values{"username": {"demo"}, "password": {"correct horse"}})
	if code, _, h := b.submit("/g/KIUKI-25-3/members", "/g/KIUKI-25-3/leave", url.Values{}); code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("the real demo leaves its group: %d %s", code, h.Get("Location"))
	}
}
