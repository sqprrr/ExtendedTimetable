package server_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

var inviteLinkRe = regexp.MustCompile(`value="(https://[^"]+/join/([A-Za-z0-9_-]+))" readonly`)

// rootBrowser signs in as a new superadmin.
func (e *env) rootBrowser(t *testing.T) *browser {
	t.Helper()
	if err := e.svc.AdminCreateSuperadmin(context.Background(), "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	b := e.browser(t)
	if code, _, _ := b.submit("/login", "/login", url.Values{"username": {"root"}, "password": {"correct horse"}}); code != http.StatusSeeOther {
		t.Fatalf("root login: %d", code)
	}
	return b
}

func TestOpenRegistrationIsGone(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)
	if code, _, _ := b.get("/register"); code != http.StatusNotFound {
		t.Fatalf("GET /register: %d", code)
	}
	if code, _, _ := b.get("/register?group=KIUKI-25-3"); code != http.StatusNotFound {
		t.Fatalf("GET /register?group=: %d", code)
	}
	if _, body, _ := b.get("/login"); strings.Contains(body, `href="/register"`) || !strings.Contains(body, "ask your group leader") {
		t.Fatalf("login page should explain registration by invitation:\n%s", body)
	}
	code, body, _ := b.get("/join/not-a-token")
	if code != http.StatusNotFound || !strings.Contains(body, "invalid or has expired") || strings.Contains(body, `name="username"`) {
		t.Fatalf("bad invite: %d\n%s", code, body)
	}
}

func TestJoinWithAnExistingAccount(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.AdminCreateGroup(ctx, "OTHER-1", "Other group", nil); err != nil {
		t.Fatal(err)
	}
	alice := e.signUp(t, "alice", store.RoleStudent)
	other := e.joinPath(t, "OTHER-1")

	// Signed in and in another group: the page explains instead of offering to join.
	_, body, _ := alice.get(other)
	if !strings.Contains(body, "already in the group KIUKI-25-3") || strings.Contains(body, "Join the group</button>") {
		t.Fatalf("invite page for a member of another group:\n%s", body)
	}
	if code, body, _ := alice.submit(other, other, url.Values{}); code != http.StatusUnprocessableEntity || !strings.Contains(body, "already in the group") {
		t.Fatalf("join a second group: %d", code)
	}
	// The own group's link leads to the group.
	if code, _, h := alice.get(e.joinPath(t, "KIUKI-25-3")); code != http.StatusSeeOther || h.Get("Location") != "/g/KIUKI-25-3" {
		t.Fatalf("own invite: %d %s", code, h.Get("Location"))
	}

	// After leaving, logging in on another group's invite page joins it.
	if code, _, h := alice.submit("/g/KIUKI-25-3/members", "/g/KIUKI-25-3/leave", url.Values{}); code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("leave: %d %s", code, h.Get("Location"))
	}
	if _, body, _ := alice.get("/"); !strings.Contains(body, "You are not in a group") {
		t.Fatalf("home after leaving:\n%s", body)
	}
	b := e.browser(t)
	if code, body, _ := b.submit(other, other+"/login", url.Values{"login_username": {"alice"}, "login_password": {"wrong password"}}); code != http.StatusUnprocessableEntity || !strings.Contains(body, "Invalid username or password") {
		t.Fatalf("bad login on invite page: %d", code)
	}
	code, _, h := b.submit(other, other+"/login", url.Values{"login_username": {"alice"}, "login_password": {"correct horse"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/g/OTHER-1" {
		t.Fatalf("login and join: %d %s", code, h.Get("Location"))
	}
	if code, _, _ := b.get("/g/OTHER-1"); code != http.StatusOK {
		t.Fatalf("new group page: %d", code)
	}
}

func TestLeaderFlowOnTheWeb(t *testing.T) {
	e := newEnv(t)
	ann := e.signUp(t, "ann", store.RoleStudent)
	bob := e.signUp(t, "bob", store.RoleStudent)
	const g = "/g/KIUKI-25-3"

	// Without a leader, members are offered the role on the overview.
	_, body, _ := ann.get(g)
	if !strings.Contains(body, `action="`+g+`/leader/claim"`) {
		t.Fatalf("overview should offer the leader role:\n%s", body)
	}
	// Students do not see the invite link.
	if _, body, _ := ann.get(g + "/members"); inviteLinkRe.MatchString(body) || strings.Contains(body, "/remove") {
		t.Fatal("a student sees the invite link or remove buttons")
	}
	if code, _, h := ann.submit(g, g+"/leader/claim", url.Values{}); code != http.StatusSeeOther || h.Get("Location") != g+"/members" {
		t.Fatalf("claim: %d %s", code, h.Get("Location"))
	}
	if code, body, _ := bob.submit(g+"/members", g+"/leader/claim", url.Values{}); code != http.StatusConflict || !strings.Contains(body, "already has a leader") {
		t.Fatalf("second claim: %d", code)
	}
	if _, body, _ := bob.get(g); strings.Contains(body, "/leader/claim") {
		t.Fatal("the overview still offers the role")
	}

	// The leader sees the full link and can replace it.
	_, body, _ = ann.get(g + "/members")
	m := inviteLinkRe.FindStringSubmatch(body)
	if m == nil || !strings.HasPrefix(m[1], e.srv.URL+"/join/") {
		t.Fatalf("leader should see the invite link:\n%s", body)
	}
	oldToken := m[2]
	if code, _, _ := ann.submit(g+"/members", g+"/invite", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("regenerate: %d", code)
	}
	if code, _, _ := e.browser(t).get("/join/" + oldToken); code != http.StatusNotFound {
		t.Fatalf("old link after regenerate: %d", code)
	}

	// The leader cannot leave, but can remove a member.
	if code, body, _ := ann.submit(g+"/members", g+"/leave", url.Values{}); code != http.StatusConflict || !strings.Contains(body, "Give up the leader role") {
		t.Fatalf("leader leaves: %d", code)
	}
	bobID := userID(t, e, "bob")
	if code, _, _ := ann.submit(g+"/members", g+"/members/"+bobID+"/remove", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("remove bob: %d", code)
	}
	if code, _, _ := bob.get(g); code != http.StatusForbidden {
		t.Fatalf("removed member opens the group: %d", code)
	}

	if code, _, _ := ann.submit(g+"/members", g+"/leader/resign", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("resign: %d", code)
	}
	if _, body, _ := ann.get(g + "/members"); !strings.Contains(body, "no leader yet") || inviteLinkRe.MatchString(body) {
		t.Fatal("after resigning the group has no leader and ann no link")
	}
}

func TestAdminPanel(t *testing.T) {
	e := newEnv(t)
	stud := e.signUp(t, "stud", store.RoleStudent)
	if code, _, _ := stud.get("/admin/groups"); code != http.StatusForbidden {
		t.Fatalf("student opens the panel: %d", code)
	}
	if code, _, _ := stud.submit("/", "/admin/groups", url.Values{"code": {"X-1"}}); code != http.StatusForbidden {
		t.Fatalf("student creates a group: %d", code)
	}

	root := e.rootBrowser(t)
	_, body, _ := root.get("/")
	if !strings.Contains(body, `<a class="nav-item" href="/admin/groups">`) || !strings.Contains(body, "<span>Groups</span>") {
		t.Fatal("the navigation should link to the panel")
	}
	code, body, _ := root.submit("/admin/groups", "/admin/groups", url.Values{"code": {"new-1"}, "name": {"New group"}, "cist_id": {"abc"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "CIST id must be a positive number") || !strings.Contains(body, `value="New group"`) {
		t.Fatalf("bad CIST id: %d", code)
	}
	code, _, h := root.submit("/admin/groups", "/admin/groups", url.Values{"code": {"new-1"}, "name": {"New group"}, "cist_id": {"42"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/g/NEW-1/members" {
		t.Fatalf("create group: %d %s", code, h.Get("Location"))
	}
	if code, body, _ := root.submit("/admin/groups", "/admin/groups", url.Values{"code": {"NEW-1"}}); code != http.StatusUnprocessableEntity || !strings.Contains(body, "already exists") {
		t.Fatalf("duplicate group: %d", code)
	}
	_, body, _ = root.get("/g/NEW-1/members")
	if !inviteLinkRe.MatchString(body) || !strings.Contains(body, "Group created") {
		t.Fatalf("new group's members page should show the link and the log:\n%s", body)
	}
	if _, body, _ := root.get("/admin/groups"); !strings.Contains(body, "New group") || !strings.Contains(body, "KIUKI-25-3") {
		t.Fatalf("panel lists the groups:\n%s", body)
	}

	// The superadmin hands the leader role over and takes it away.
	studID := userID(t, e, "stud")
	const g = "/g/KIUKI-25-3"
	if code, _, _ := root.submit(g+"/members", g+"/leader", url.Values{"user_id": {studID}}); code != http.StatusSeeOther {
		t.Fatalf("set leader: %d", code)
	}
	if _, body, _ := stud.get("/"); !strings.Contains(body, "<strong>leader</strong>") {
		t.Fatal("stud should be the leader")
	}
	if code, _, _ := stud.submit(g+"/members", g+"/leader", url.Values{"user_id": {studID}}); code != http.StatusForbidden {
		t.Fatalf("leader uses the superadmin action: %d", code)
	}
	if code, _, _ := root.submit(g+"/members", g+"/leader/remove", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("remove leader: %d", code)
	}
	_, body, _ = root.get(g + "/members")
	for _, want := range []string{"Made the leader", "Leader role taken away", "Group log"} {
		if !strings.Contains(body, want) {
			t.Errorf("log lacks %q", want)
		}
	}
	if _, body, _ := stud.get(g + "/members"); strings.Contains(body, "Group log") {
		t.Fatal("only superadmins see the log")
	}
}

func TestInviteTokenIsNotLogged(t *testing.T) {
	e := newEnv(t)
	logs := captureLogs(t, slog.LevelInfo)
	join := e.joinPath(t, "KIUKI-25-3")
	b := e.browser(t)
	b.register("KIUKI-25-3", "alice")
	token := strings.TrimPrefix(join, "/join/")
	if strings.Contains(logs.String(), token) {
		t.Fatalf("the invite token was logged:\n%s", logs.String())
	}
	found := false
	for _, r := range logs.records(t, "request") {
		found = found || r["path"] == "/join/…/register"
	}
	if !found {
		t.Fatalf("no masked request line:\n%s", logs.String())
	}
}

// userID returns a member's id as a string, for form values and paths.
func userID(t *testing.T, e *env, username string) string {
	t.Helper()
	u, err := e.st.UserByUsername(context.Background(), username)
	if err != nil {
		t.Fatal(err)
	}
	return strconv.FormatInt(u.ID, 10)
}
