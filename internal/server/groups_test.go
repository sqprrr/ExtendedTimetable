package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// signUp registers username into KIUKI-25-3 and returns their browser.
func (e *env) signUp(t *testing.T, username string, role store.Role) *browser {
	t.Helper()
	b := e.browser(t)
	code, _, _ := b.submit("/register", "/register", url.Values{
		"group": {"KIUKI-25-3"}, "username": {username}, "password": {"correct horse"}, "password_confirm": {"correct horse"},
	})
	if code != http.StatusSeeOther {
		t.Fatalf("register %s: %d", username, code)
	}
	if role == store.RoleLeader {
		if err := e.svc.AdminSetRole(context.Background(), username, "KIUKI-25-3", role); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

var subjectOptionRe = regexp.MustCompile(`<option value="(\d+)"[^>]*>Physics</option>`)

func TestLeaderManagesGroupPages(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	const g = "/g/KIUKI-25-3"

	if _, body, _ := lead.get("/"); !strings.Contains(body, `href="`+g+`"`) {
		t.Fatal("home should link to the group page")
	}

	// Subjects
	if code, _, h := lead.submit(g+"/subjects", g+"/subjects", url.Values{"name": {"Physics"}, "short_name": {"PH"}}); code != http.StatusSeeOther || h.Get("Location") != g+"/subjects" {
		t.Fatalf("create subject: %d %s", code, h.Get("Location"))
	}
	code, body, _ := lead.submit(g+"/subjects", g+"/subjects", url.Values{"name": {"physics"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "already has a subject") || !strings.Contains(body, `value="physics"`) {
		t.Fatalf("duplicate subject: %d", code)
	}
	_, body, _ = lead.get(g + "/links")
	m := subjectOptionRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("subject picker missing:\n%s", body)
	}
	subjectID := m[1]

	// Class link
	if code, _, _ := lead.submit(g+"/links", g+"/links", url.Values{
		"subject_id": {subjectID}, "lesson_type": {"lab"}, "url": {"https://meet.example/lab"}, "note": {"subgroup 1"},
	}); code != http.StatusSeeOther {
		t.Fatalf("create class link: %d", code)
	}
	code, body, _ = lead.submit(g+"/links", g+"/links", url.Values{
		"subject_id": {subjectID}, "lesson_type": {"lab"}, "url": {"javascript:alert(1)"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "http:// or https://") {
		t.Fatalf("bad class link: %d", code)
	}

	// Homework, with Markdown that must be sanitized.
	if code, _, _ := lead.submit(g+"/homework", g+"/homework", url.Values{
		"subject_id": {subjectID}, "title": {"Lab <1>"}, "description": {"Do **this**<script>alert(1)</script>"},
		"due_at": {"2099-09-01T09:30"}, "max_points": {"7,5"}, "links": {"Manual — https://example.com/m.pdf\n\nhttps://example.com/x"},
	}); code != http.StatusSeeOther {
		t.Fatalf("create homework: %d", code)
	}
	_, body, _ = stud.get(g + "/homework")
	hwRe := regexp.MustCompile(`href="` + g + `/homework/(\d+)"><strong>Lab &lt;1&gt;</strong>`)
	hm := hwRe.FindStringSubmatch(body)
	if hm == nil || !strings.Contains(body, "max 7.5 pts") || !strings.Contains(body, "01.09.2099 09:30") {
		t.Fatalf("student homework list:\n%s", body)
	}
	hwURL := g + "/homework/" + hm[1]
	_, body, _ = stud.get(hwURL)
	if !strings.Contains(body, "<strong>this</strong>") || strings.Contains(body, "<script>alert") {
		t.Fatalf("homework detail markdown:\n%s", body)
	}
	if !strings.Contains(body, `>Manual</a>`) || !strings.Contains(body, `>https://example.com/x</a>`) {
		t.Fatalf("homework links:\n%s", body)
	}
	if strings.Contains(body, "/edit") {
		t.Fatal("students should not see edit links")
	}

	// The edit form is prefilled.
	code, body, _ = lead.get(hwURL + "/edit")
	if code != http.StatusOK || !strings.Contains(body, `value="2099-09-01T09:30"`) || !strings.Contains(body, "Manual https://example.com/m.pdf") {
		t.Fatalf("edit homework form: %d\n%s", code, body)
	}

	// Note and recording
	if code, _, _ := lead.submit(g+"/notes", g+"/notes", url.Values{"title": {"Exam moved"}, "body": {"To *Friday*"}, "pinned": {"on"}}); code != http.StatusSeeOther {
		t.Fatalf("create note: %d", code)
	}
	if code, _, _ := lead.submit(g+"/resources", g+"/resources", url.Values{
		"subject_id": {subjectID}, "kind": {"recording"}, "title": {"Lecture 1"}, "url": {"https://youtu.be/abc"}, "date": {"2026-09-01"},
	}); code != http.StatusSeeOther {
		t.Fatalf("create resource: %d", code)
	}

	// The overview shows it all to the student.
	code, body, _ = stud.get(g)
	for _, want := range []string{"Lab &lt;1&gt;", "Exam moved", "<em>Friday</em>", "subgroup 1", "https://meet.example/lab"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview missing %q", want)
		}
	}
	if code != http.StatusOK {
		t.Fatalf("overview: %d", code)
	}
	if _, body, _ := stud.get(g + "/resources"); !strings.Contains(body, "Lecture 1") || !strings.Contains(body, "2026-09-01") {
		t.Fatal("student should see recordings")
	}

	// Students cannot write, even by posting directly.
	if code, _, _ := stud.submit(g+"/notes", g+"/notes", url.Values{"title": {"hack"}}); code != http.StatusForbidden {
		t.Fatalf("student create note: %d", code)
	}
	if code, _, _ := stud.submit(g, hwURL+"/delete", url.Values{}); code != http.StatusForbidden {
		t.Fatalf("student delete homework: %d", code)
	}
	if code, _, _ := stud.get(hwURL + "/edit"); code != http.StatusForbidden {
		t.Fatalf("student edit page: %d", code)
	}

	// A subject in use cannot be deleted; once it is free it can.
	if code, body, _ := lead.submit(g+"/subjects", g+"/subjects/"+subjectID+"/delete", url.Values{}); code != http.StatusConflict || !strings.Contains(body, "still has") {
		t.Fatalf("delete used subject: %d", code)
	}
	if code, _, _ := lead.submit(g, hwURL+"/delete", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("delete homework: %d", code)
	}
	if code, _, _ := stud.get(hwURL); code != http.StatusNotFound {
		t.Fatalf("deleted homework: %d", code)
	}
}

func TestGroupPagesNeedMembership(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.AdminCreateGroup(context.Background(), "OTHER-1", "", nil); err != nil {
		t.Fatal(err)
	}
	b := e.browser(t)
	if code, _, h := b.get("/g/KIUKI-25-3"); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("anonymous: %d %s", code, h.Get("Location"))
	}
	stud := e.signUp(t, "stud", store.RoleStudent)
	if code, _, _ := stud.get("/g/OTHER-1/notes"); code != http.StatusForbidden {
		t.Fatalf("non-member: %d", code)
	}
	if code, _, _ := stud.get("/g/NOPE"); code != http.StatusNotFound {
		t.Fatalf("unknown group: %d", code)
	}
	if code, _, _ := stud.get("/g/KIUKI-25-3/homework/abc"); code != http.StatusNotFound {
		t.Fatalf("bad id: %d", code)
	}
}

func (b *browser) api(method, path, csrf string, body any) (int, []byte) {
	b.t.Helper()
	var rd io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, b.e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (b *browser) csrf() string {
	b.t.Helper()
	code, body := b.api("GET", "/api/v1/me", "", nil)
	var me struct {
		CSRFToken string `json:"csrf_token"`
	}
	if code != http.StatusOK || json.Unmarshal(body, &me) != nil {
		b.t.Fatalf("/api/v1/me: %d %s", code, body)
	}
	return me.CSRFToken
}

func TestAPIGroupContent(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	const g = "/api/v1/groups/KIUKI-25-3"
	tok := lead.csrf()

	code, body := lead.api("POST", g+"/subjects", tok, map[string]any{"name": "Physics"})
	var sub struct{ ID int64 }
	if code != http.StatusCreated || json.Unmarshal(body, &sub) != nil {
		t.Fatalf("create subject: %d %s", code, body)
	}
	if code, body := lead.api("POST", g+"/subjects", "", map[string]any{"name": "x"}); code != http.StatusForbidden {
		t.Fatalf("missing CSRF header: %d %s", code, body)
	}

	code, body = lead.api("POST", g+"/homework", tok, map[string]any{
		"subject_id": sub.ID, "title": "Lab 1", "description_md": "**x**", "due_at": "2099-01-01T10:00:00Z", "max_points": 5,
		"links": []map[string]string{{"title": "M", "url": "https://example.com"}},
	})
	var hw struct{ ID int64 }
	if code != http.StatusCreated || json.Unmarshal(body, &hw) != nil {
		t.Fatalf("create homework: %d %s", code, body)
	}
	code, body = lead.api("POST", g+"/homework", tok, map[string]any{"subject_id": sub.ID, "title": ""})
	if code != http.StatusUnprocessableEntity || !strings.Contains(string(body), `"field":"title"`) {
		t.Fatalf("invalid homework: %d %s", code, body)
	}

	stok := stud.csrf()
	code, body = stud.api("GET", g+"/homework/"+itoa(hw.ID), stok, nil)
	var got struct {
		Title           string
		SubjectName     string `json:"subject_name"`
		DescriptionHTML string `json:"description_html"`
		Links           []struct{ URL string }
	}
	if code != http.StatusOK || json.Unmarshal(body, &got) != nil {
		t.Fatalf("get homework: %d %s", code, body)
	}
	if got.Title != "Lab 1" || got.SubjectName != "Physics" || !strings.Contains(got.DescriptionHTML, "<strong>x</strong>") || len(got.Links) != 1 {
		t.Fatalf("homework JSON: %s", body)
	}
	if code, _ := stud.api("DELETE", g+"/homework/"+itoa(hw.ID), stok, nil); code != http.StatusForbidden {
		t.Fatalf("student delete: %d", code)
	}
	if code, _ := lead.api("DELETE", g+"/subjects/"+itoa(sub.ID), tok, nil); code != http.StatusConflict {
		t.Fatalf("delete used subject: %d", code)
	}
	if code, _ := lead.api("DELETE", g+"/homework/"+itoa(hw.ID), tok, nil); code != http.StatusNoContent {
		t.Fatalf("delete homework: %d", code)
	}
	if code, body := stud.api("GET", g+"/homework", stok, nil); code != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("empty list: %d %s", code, body)
	}
	if code, _ := stud.api("GET", "/api/v1/groups/NOPE/notes", stok, nil); code != http.StatusNotFound {
		t.Fatalf("unknown group: %d", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
