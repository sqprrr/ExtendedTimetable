package server_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

type fakeGroupList []cist.Group

func (l fakeGroupList) FindGroups(_ context.Context, query string) ([]cist.Group, error) {
	var out []cist.Group
	for _, g := range l {
		if strings.Contains(strings.ToLower(g.Name), strings.ToLower(query)) {
			out = append(out, g)
		}
	}
	return out, nil
}

func TestGroupRequestFlow(t *testing.T) {
	e := newEnvWith(t, service.Config{CISTGroups: fakeGroupList{{ID: 11881842, Name: "КІУКІ-25-3"}}})
	visitor := e.browser(t)

	if _, body, _ := visitor.get("/login"); !strings.Contains(body, `<a href="/request">Send a request</a>`) {
		t.Fatalf("the login page should link to the request form:\n%s", body)
	}
	code, body, _ := visitor.get("/request")
	if code != http.StatusOK || !strings.Contains(body, `<option value="ХНУРЕ"`) || !strings.Contains(body, `name="extt_hp"`) {
		t.Fatalf("form: %d\n%s", code, body)
	}

	// A rejected form keeps what was typed.
	form := url.Values{"university": {"ХНУРЕ"}, "group": {"КІУКІ-25-3"}, "telegram": {"+380501234567"}, "comment": {"<b>28</b> people"}}
	code, body, _ = visitor.submit("/request", "/request", form)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Enter your Telegram username") ||
		!strings.Contains(body, `value="КІУКІ-25-3"`) || !strings.Contains(body, "&lt;b&gt;28&lt;/b&gt; people</textarea>") {
		t.Fatalf("bad Telegram: %d\n%s", code, body)
	}

	// A bot that fills in the hidden field is told it worked, and nothing is kept.
	bot := url.Values{"university": {"ХНУРЕ"}, "group": {"spam"}, "telegram": {"spam_bot"}, "extt_hp": {"https://spam.example"}}
	if code, _, h := visitor.submit("/request", "/request", bot); code != http.StatusSeeOther || h.Get("Location") != "/request?sent=1" {
		t.Fatalf("bot: %d %s", code, h.Get("Location"))
	}

	form.Set("telegram", "https://t.me/leader_25")
	if code, _, h := visitor.submit("/request", "/request", form); code != http.StatusSeeOther || h.Get("Location") != "/request?sent=1" {
		t.Fatalf("send: %d %s", code, h.Get("Location"))
	}
	if _, body, _ = visitor.get("/request?sent=1"); !strings.Contains(body, "We have received your request") || strings.Contains(body, `action="/request"`) {
		t.Fatalf("sent page:\n%s", body)
	}

	// Signed-in users can send one too; only superadmins read them.
	stud := e.signUp(t, "stud", store.RoleStudent)
	if code, _, _ := stud.get("/request"); code != http.StatusOK {
		t.Fatalf("signed-in form: %d", code)
	}
	if code, _, _ := stud.get("/admin/requests"); code != http.StatusForbidden {
		t.Fatalf("student list: %d", code)
	}
	if code, _, h := visitor.get("/admin/requests"); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("anonymous list: %d %s", code, h.Get("Location"))
	}

	if err := e.svc.AdminCreateSuperadmin(context.Background(), "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	root := e.browser(t)
	root.submit("/login", "/login", url.Values{"username": {"root"}, "password": {"correct horse"}})
	_, body, _ = root.get("/admin/requests")
	m := regexp.MustCompile(`action="(/admin/requests/\d+)/resolve"`).FindAllStringSubmatch(body, -1)
	if len(m) != 1 || !strings.Contains(body, "Open (1)") || !strings.Contains(body, `href="https://t.me/leader_25"`) ||
		!strings.Contains(body, "CIST: КІУКІ-25-3 (11881842)") || !strings.Contains(body, "&lt;b&gt;28&lt;/b&gt; people") ||
		!strings.Contains(body, `href="/admin/requests"`) {
		t.Fatalf("list:\n%s", body)
	}
	if code, _, h := root.submit("/admin/requests", m[0][1]+"/resolve", url.Values{"resolved": {"1"}}); code != http.StatusSeeOther || h.Get("Location") != "/admin/requests" {
		t.Fatalf("resolve: %d %s", code, h.Get("Location"))
	}
	if _, body, _ = root.get("/admin/requests"); !strings.Contains(body, "No open requests.") {
		t.Fatalf("a resolved request should leave the open list:\n%s", body)
	}
	if code, _, h := root.submit("/admin/requests?show=all", m[0][1]+"/delete", url.Values{"show": {"all"}}); code != http.StatusSeeOther || h.Get("Location") != "/admin/requests?show=all" {
		t.Fatalf("delete: %d %s", code, h.Get("Location"))
	}
	if _, body, _ = root.get("/admin/requests?show=all"); !strings.Contains(body, "No requests yet.") {
		t.Fatalf("after delete:\n%s", body)
	}
}

func TestCreateGroupFromRequest(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeCIST{events: []cist.Event{{Start: now.Add(time.Hour), End: now.Add(2 * time.Hour),
		Subject: "ООП", Type: "Лк", Room: "DL", Groups: "КІУКІ-25-4"}}}
	e := newEnvWith(t, service.Config{CIST: src, CISTGroups: fakeGroupList{{ID: 11881844, Name: "КІУКІ-25-4"}}})
	visitor := e.browser(t)
	visitor.submit("/request", "/request", url.Values{"university": {"ХНУРЕ"}, "group": {"кіукі-25-4"}, "telegram": {"@leader_25"}})

	if err := e.svc.AdminCreateSuperadmin(context.Background(), "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	root := e.browser(t)
	root.submit("/login", "/login", url.Values{"username": {"root"}, "password": {"correct horse"}})
	_, body, _ := root.get("/admin/requests")
	m := regexp.MustCompile(`href="(/admin/groups\?request=\d+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no Create group button:\n%s", body)
	}
	// The panel's form comes filled in from the request.
	_, body, _ = root.get(m[1])
	for _, want := range []string{`name="code" value="KIUKI-25-4"`, `name="name" value="КІУКІ-25-4"`, `name="cist_id" value="11881844"`,
		`name="request_id" value="`, "From the request: кіукі-25-4, ХНУРЕ, @leader_25"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in the form:\n%s", want, body)
		}
	}
	id := regexp.MustCompile(`name="request_id" value="(\d+)"`).FindStringSubmatch(body)[1]
	code, _, h := root.submit(m[1], "/admin/groups", url.Values{"code": {"KIUKI-25-4"}, "name": {"КІУКІ-25-4"}, "cist_id": {"11881844"}, "request_id": {id}})
	if code != http.StatusSeeOther || h.Get("Location") != "/g/KIUKI-25-4/members?request="+id {
		t.Fatalf("create: %d %s", code, h.Get("Location"))
	}
	// The members page shows the invite link and whom to send it to.
	_, body, _ = root.get(h.Get("Location"))
	if !strings.Contains(body, `<a href="https://t.me/leader_25" target="_blank" rel="noopener noreferrer">@leader_25</a>`) || !strings.Contains(body, "/join/") {
		t.Fatalf("members page:\n%s", body)
	}
	// The request is resolved, and the timetable with its subjects is there already.
	if _, body, _ = root.get("/admin/requests"); !strings.Contains(body, "No open requests.") {
		t.Fatalf("the request should be resolved:\n%s", body)
	}
	if _, body, _ = root.get("/g/KIUKI-25-4/subjects"); !strings.Contains(body, "ООП") {
		t.Fatalf("subjects from CIST:\n%s", body)
	}
}
