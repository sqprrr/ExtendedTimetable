package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// htmx posts a form the way htmx does: with the HX-Request and CSRF headers.
func (b *browser) htmx(path, csrf string, fields url.Values) (int, string) {
	b.t.Helper()
	req, _ := http.NewRequest("POST", b.e.srv.URL+path, strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// setupHomework makes lead the leader, adds a subject and one assignment
// worth 10 points, and returns the assignment's page.
func setupHomework(t *testing.T, e *env, lead *browser) string {
	t.Helper()
	const g = "/g/KIUKI-25-3"
	lead.submit(g+"/subjects", g+"/subjects", url.Values{"name": {"Physics"}})
	_, body, _ := lead.get(g + "/homework")
	m := subjectOptionRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no subject option")
	}
	if code, _, _ := lead.submit(g+"/homework", g+"/homework", url.Values{
		"subject_id": {m[1]}, "title": {"Lab 1"}, "max_points": {"10"},
	}); code != http.StatusSeeOther {
		t.Fatalf("create homework: %d", code)
	}
	_, body, _ = lead.get(g + "/homework")
	hm := regexp.MustCompile(`<a class="row-title" href="(` + g + `/homework/\d+)">Lab 1<`).FindStringSubmatch(body)
	if hm == nil {
		t.Fatalf("homework link missing:\n%s", body)
	}
	return hm[1]
}

func TestStatusToggleWithHtmx(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	hwURL := setupHomework(t, e, lead)

	_, body, _ := stud.get("/g/KIUKI-25-3/homework")
	if !strings.Contains(body, `class="status-check" data-status="not_started"`) || !strings.Contains(body, `name="status" value="in_progress"`) {
		t.Fatalf("list should offer the toggle:\n%s", body)
	}
	if !strings.Contains(body, `<script src="/static/htmx.min.js?v=`) {
		t.Fatal("htmx is not loaded")
	}
	tok := stud.csrf()

	// htmx gets just the list item back, already showing the new status.
	code, frag := stud.htmx(hwURL+"/progress", tok, url.Values{"view": {"item"}, "from": {"list"}, "status": {"in_progress"}})
	if code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(frag), `<li class="hw-row`) || strings.Contains(frag, "<html") {
		t.Fatalf("htmx toggle: %d\n%s", code, frag)
	}
	if !strings.Contains(frag, `data-status="in_progress"`) || !strings.Contains(frag, "In progress. Mark as Done") || !strings.Contains(frag, `name="status" value="done"`) {
		t.Fatalf("fragment should show in progress and offer done:\n%s", frag)
	}

	// Without JavaScript the same form redirects back to the page it was on.
	code, _, h := stud.submit("/g/KIUKI-25-3", hwURL+"/progress", url.Values{"view": {"item"}, "from": {"overview"}, "status": {"done"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/g/KIUKI-25-3" {
		t.Fatalf("plain toggle: %d %s", code, h.Get("Location"))
	}
	if _, body, _ := stud.get(hwURL); !strings.Contains(body, `class="status status-done" aria-pressed="true"`) {
		t.Fatalf("detail page should show done:\n%s", body)
	}

	// A rejected toggle answers htmx with the message as plain text.
	req, _ := http.NewRequest("POST", e.srv.URL+hwURL+"/progress", strings.NewReader(url.Values{"view": {"item"}, "status": {"finished"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", tok)
	resp, err := stud.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") ||
		!strings.Contains(string(msg), "Choose not started, in progress or done") {
		t.Fatalf("rejected toggle: %d %s %q", resp.StatusCode, resp.Header.Get("Content-Type"), msg)
	}

	// The panel also refreshes the Overdue badge out of band.
	code, frag = stud.htmx(hwURL+"/progress", tok, url.Values{"view": {"panel"}, "status": {"done"}})
	if code != http.StatusOK || !strings.Contains(frag, `id="overdue-badge" hx-swap-oob="true"`) {
		t.Fatalf("panel should update the badge: %d\n%s", code, frag)
	}

	// The leader's own tracker is untouched.
	if _, body, _ := lead.get(hwURL); !strings.Contains(body, `class="status status-not_started" aria-pressed="true"`) {
		t.Fatal("the leader must not see the student's status")
	}
}

func TestGradeFormAndMyGrades(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	hwURL := setupHomework(t, e, lead)
	tok := stud.csrf()

	// A grade above max points comes back in the panel, with what was typed.
	code, frag := stud.htmx(hwURL+"/progress", tok, url.Values{"view": {"panel"}, "grade": {"12"}})
	if code != http.StatusOK || !strings.Contains(frag, `id="progress"`) || !strings.Contains(frag, "more than the 10 max points") || !strings.Contains(frag, `value="12"`) {
		t.Fatalf("too high grade: %d\n%s", code, frag)
	}
	code, frag = stud.htmx(hwURL+"/progress", tok, url.Values{"view": {"panel"}, "grade": {"7,5"}})
	if code != http.StatusOK || !strings.Contains(frag, `value="7.5"`) || strings.Contains(frag, `class="error"`) {
		t.Fatalf("save grade: %d\n%s", code, frag)
	}

	// Without JavaScript a bad grade shows the page with the error in the
	// panel and what was typed.
	code, body, _ := stud.submit(hwURL, hwURL+"/progress", url.Values{"view": {"panel"}, "grade": {"abc"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Grade must be a number") || !strings.Contains(body, `value="abc"`) {
		t.Fatalf("plain bad grade: %d\n%s", code, body)
	}

	_, body, _ = stud.get("/g/KIUKI-25-3/grades")
	if !strings.Contains(body, "My grades") || !strings.Contains(body, "7.5 / 10") || !strings.Contains(body, "75%") {
		t.Fatalf("my grades:\n%s", body)
	}
	// (Not just "7.5": icon paths hold numbers like that.)
	if _, body, _ := lead.get("/g/KIUKI-25-3/grades"); strings.Contains(body, "7.5 / 10") || strings.Contains(body, "75%") {
		t.Fatal("the leader must not see the student's grades")
	}
	if _, body, _ := lead.get(hwURL); strings.Contains(body, `value="7.5"`) {
		t.Fatal("the leader must not see the student's grade on the homework page")
	}
}

func TestSuperadminSeesNoTracker(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	hwURL := setupHomework(t, e, lead)
	if err := e.svc.AdminCreateSuperadmin(context.Background(), "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	root := e.browser(t)
	root.submit("/login", "/login", url.Values{"username": {"root"}, "password": {"correct horse"}})

	_, body, _ := root.get(hwURL)
	if strings.Contains(body, `id="progress"`) || strings.Contains(body, "My grades") {
		t.Fatal("a superadmin who is not a member has no tracker")
	}
	if code, _, _ := root.get("/g/KIUKI-25-3/grades"); code != http.StatusForbidden {
		t.Fatalf("superadmin grades page: %d", code)
	}
	if code, _, _ := root.submit(hwURL, hwURL+"/progress", url.Values{"status": {"done"}}); code != http.StatusForbidden {
		t.Fatalf("superadmin progress: %d", code)
	}
}

func TestAPIProgress(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	hwURL := setupHomework(t, e, lead)
	id := hwURL[strings.LastIndex(hwURL, "/")+1:]
	api := "/api/v1/groups/KIUKI-25-3"
	tok := stud.csrf()

	code, body := stud.api("PUT", api+"/homework/"+id+"/progress", tok, map[string]any{"grade": 6})
	if code != http.StatusOK || !strings.Contains(string(body), `"progress":{"status":"not_started","grade":6}`) {
		t.Fatalf("set grade: %d %s", code, body)
	}
	code, body = stud.api("PUT", api+"/homework/"+id+"/progress", tok, map[string]any{"status": "done"})
	if code != http.StatusOK || !strings.Contains(string(body), `"progress":{"status":"done","grade":6}`) {
		t.Fatalf("set status keeps grade: %d %s", code, body)
	}
	if code, body := stud.api("PUT", api+"/homework/"+id+"/progress", tok, map[string]any{"grade": 11}); code != http.StatusUnprocessableEntity {
		t.Fatalf("grade over max: %d %s", code, body)
	}
	// A status-only PUT does not re-check the stored grade.
	ltok := lead.csrf()
	if code, body := lead.api("PUT", api+"/homework/"+id, ltok, map[string]any{"max_points": 4}); code != http.StatusOK {
		t.Fatalf("lower max points: %d %s", code, body)
	}
	if code, body := stud.api("PUT", api+"/homework/"+id+"/progress", tok, map[string]any{"status": "in_progress"}); code != http.StatusOK ||
		!strings.Contains(string(body), `"grade":6`) {
		t.Fatalf("status-only PUT after max points dropped: %d %s", code, body)
	}
	if code, body := stud.api("PUT", api+"/homework/"+id+"/progress", tok, map[string]any{"status": "done"}); code != http.StatusOK {
		t.Fatalf("restore status: %d %s", code, body)
	}
	if code, body := lead.api("PUT", api+"/homework/"+id, ltok, map[string]any{"max_points": 10}); code != http.StatusOK {
		t.Fatalf("restore max points: %d %s", code, body)
	}

	if code, body := stud.api("PUT", api+"/homework/"+id+"/progress", tok, map[string]any{"grade": nil}); code != http.StatusOK ||
		!strings.Contains(string(body), `"progress":{"status":"done","grade":null}`) {
		t.Fatalf("null clears the grade: %d %s", code, body)
	}
	if code, _ := stud.api("PUT", api+"/homework/"+id+"/progress", tok, map[string]any{"grade": 6}); code != http.StatusOK {
		t.Fatalf("set grade again: %d", code)
	}
	code, body = stud.api("GET", api+"/grades", tok, nil)
	var g struct {
		Subjects []struct {
			SubjectName string  `json:"subject_name"`
			Earned      float64 `json:"earned"`
			Max         float64 `json:"max"`
		} `json:"subjects"`
		Overall struct{ Graded, Assignments int } `json:"overall"`
	}
	if code != http.StatusOK || json.Unmarshal(body, &g) != nil {
		t.Fatalf("grades: %d %s", code, body)
	}
	if len(g.Subjects) != 1 || g.Subjects[0].Earned != 6 || g.Subjects[0].Max != 10 || g.Overall.Graded != 1 {
		t.Fatalf("grades JSON: %s", body)
	}

	// The leader's view of the same assignment carries only their own progress.
	_, body = lead.api("GET", api+"/homework/"+id, lead.csrf(), nil)
	if !strings.Contains(string(body), `"progress":{"status":"not_started","grade":null}`) {
		t.Fatalf("leader sees: %s", body)
	}
}

func TestHomeworkFilters(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	hwURL := setupHomework(t, e, lead)
	const list = "/g/KIUKI-25-3/homework"

	_, body, _ := stud.get(list)
	if !strings.Contains(body, `<form class="filters" method="get"`) || !strings.Contains(body, `<select name="status">`) {
		t.Fatalf("list should have the filters:\n%s", body)
	}
	if _, body, _ = stud.get(list + "?status=done"); strings.Contains(body, "Lab 1") || !strings.Contains(body, "No homework matches") {
		t.Fatalf("done filter should hide the not started assignment:\n%s", body)
	}
	if _, body, _ = stud.get(list + "?status=not_started"); !strings.Contains(body, "Lab 1") || !strings.Contains(body, `name="filter" value="status=not_started"`) {
		t.Fatalf("not started filter should show it and carry the filter:\n%s", body)
	}
	// Junk in the URL shows the whole list.
	if _, body, _ = stud.get(list + "?status=nope&subject_id=x"); !strings.Contains(body, "Lab 1") {
		t.Fatalf("bad filter values should be ignored:\n%s", body)
	}

	// Without JavaScript a toggle returns to the filtered list.
	code, _, h := stud.submit(list+"?status=not_started", hwURL+"/progress", url.Values{
		"view": {"item"}, "from": {"list"}, "filter": {"status=not_started&evil=1"}, "status": {"done"}})
	if code != http.StatusSeeOther || h.Get("Location") != list+"?status=not_started" {
		t.Fatalf("plain toggle: %d %s", code, h.Get("Location"))
	}
	if _, body, _ = stud.get(list + "?status=done"); !strings.Contains(body, "Lab 1") {
		t.Fatalf("done filter should show it now:\n%s", body)
	}

	tok := stud.csrf()
	if code, b := stud.api("GET", "/api/v1/groups/KIUKI-25-3/homework?status=in_progress", tok, nil); code != http.StatusOK || strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("api filter: %d %s", code, b)
	}
	if code, _ := stud.api("GET", "/api/v1/groups/KIUKI-25-3/homework?status=nope", tok, nil); code != http.StatusBadRequest {
		t.Fatalf("api bad status: %d", code)
	}
}
