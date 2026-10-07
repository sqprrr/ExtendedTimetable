package server_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestFeedbackFlow(t *testing.T) {
	e := newEnv(t)
	stud := e.signUp(t, "stud", store.RoleStudent)

	if code, _, h := e.browser(t).get("/feedback"); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("anonymous feedback page: %d %s", code, h.Get("Location"))
	}
	_, body, _ := stud.get("/")
	if !strings.Contains(body, `href="/feedback"`) || !strings.Contains(body, "<span>Feedback</span>") {
		t.Fatal("the navigation should link to the feedback page")
	}

	// A rejected form keeps what was typed.
	code, body, _ := stud.submit("/feedback", "/feedback", url.Values{"kind": {"review"}, "rating": {"9"}, "message": {"Good <b>site</b>"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Rating must be 1 to 5") || !strings.Contains(body, "Good &lt;b&gt;site&lt;/b&gt;</textarea>") {
		t.Fatalf("bad rating: %d\n%s", code, body)
	}
	code, _, h := stud.submit("/feedback", "/feedback", url.Values{"kind": {"review"}, "rating": {"4"}, "message": {"Good <b>site</b>"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/feedback?sent=1" {
		t.Fatalf("send: %d %s", code, h.Get("Location"))
	}
	_, body, _ = stud.get("/feedback?sent=1")
	if !strings.Contains(body, "Thank you!") || !strings.Contains(body, "★★★★☆") || !strings.Contains(body, "Good &lt;b&gt;site&lt;/b&gt;") {
		t.Fatalf("sent page:\n%s", body)
	}
	if code, _, _ := stud.get("/admin/feedback"); code != http.StatusForbidden {
		t.Fatalf("student inbox: %d", code)
	}

	if err := e.svc.AdminCreateSuperadmin(context.Background(), "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	root := e.browser(t)
	root.submit("/login", "/login", url.Values{"username": {"root"}, "password": {"correct horse"}})
	if _, body, _ = root.get("/feedback"); !strings.Contains(body, "Feedback inbox (1 open)") {
		t.Fatalf("superadmin should get a link to the inbox:\n%s", body)
	}
	_, body, _ = root.get("/admin/feedback")
	m := regexp.MustCompile(`action="(/admin/feedback/\d+)/resolve"`).FindStringSubmatch(body)
	if m == nil || !strings.Contains(body, "<strong>stud</strong>") {
		t.Fatalf("inbox:\n%s", body)
	}
	if code, _, h := root.submit("/admin/feedback", m[1]+"/resolve", url.Values{"resolved": {"1"}}); code != http.StatusSeeOther || h.Get("Location") != "/admin/feedback" {
		t.Fatalf("resolve: %d %s", code, h.Get("Location"))
	}
	if _, body, _ = root.get("/admin/feedback"); !strings.Contains(body, "No open feedback.") {
		t.Fatalf("resolved feedback should leave the open list:\n%s", body)
	}
	if _, body, _ = stud.get("/feedback"); !strings.Contains(body, "Resolved") {
		t.Fatalf("the author should see it resolved:\n%s", body)
	}
	if code, _, h := root.submit("/admin/feedback?show=all", m[1]+"/delete", url.Values{"show": {"all"}}); code != http.StatusSeeOther || h.Get("Location") != "/admin/feedback?show=all" {
		t.Fatalf("delete: %d %s", code, h.Get("Location"))
	}
	if _, body, _ = root.get("/admin/feedback?show=all"); !strings.Contains(body, "No feedback yet.") {
		t.Fatalf("after delete:\n%s", body)
	}
}
