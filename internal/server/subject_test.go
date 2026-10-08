package server_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestSubjectPage(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	const g = "/g/KIUKI-25-3"

	if code, _, _ := lead.submit(g+"/subjects/new", g+"/subjects", url.Values{
		"name": {"Physics"}, "short_name": {"PH"}, "lecturer": {"Bondarenko O. V."}, "dl_url": {"https://dl.nure.ua/course/1"},
	}); code != http.StatusSeeOther {
		t.Fatalf("create subject: %d", code)
	}
	if code, body, _ := lead.submit(g+"/subjects/new", g+"/subjects", url.Values{"name": {"Bad"}, "dl_url": {"not a link"}}); code != http.StatusUnprocessableEntity || !strings.Contains(body, `value="not a link"`) {
		t.Fatalf("bad DL link: %d", code)
	}
	grp, err := e.st.GroupByCode(context.Background(), "KIUKI-25-3")
	if err != nil {
		t.Fatal(err)
	}
	subs, err := e.st.ListSubjects(context.Background(), grp.ID)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subjects: %v %v", subs, err)
	}
	id := strconv.FormatInt(subs[0].ID, 10)
	page := g + "/subjects/" + id

	// The list links to the page.
	if _, body, _ := stud.get(g + "/subjects"); !strings.Contains(body, `href="`+page+`"`) {
		t.Fatalf("subjects list should link to the subject page:\n%s", body)
	}

	// A subject without class links or a DL page tells the leader where to
	// add them; students only see that there are none.
	lead.submit(g+"/subjects/new", g+"/subjects", url.Values{"name": {"Chemistry"}})
	empty, err := e.st.ListSubjects(context.Background(), grp.ID)
	if err != nil || len(empty) != 2 {
		t.Fatalf("subjects: %v %v", empty, err)
	}
	chem := g + "/subjects/" + strconv.FormatInt(empty[0].ID, 10) // ordered by name
	if _, body, _ := lead.get(chem); !strings.Contains(body, "No class links or DL page yet") {
		t.Fatal("leader should see the hint")
	}
	if _, body, _ := stud.get(chem); strings.Contains(body, "No class links or DL page yet") {
		t.Fatal("students should not see the leader's hint")
	}
	if _, body, _ := lead.get(page); strings.Contains(body, "No class links or DL page yet") {
		t.Fatal("a subject with a DL page needs no hint")
	}
	lead.submit(g+"/links/new", g+"/links", url.Values{"subject_id": {id}, "lesson_type": {"lab"}, "url": {"https://meet.example/lab"}})
	lead.submit(g+"/homework/new", g+"/homework", url.Values{"subject_id": {id}, "title": {"Lab 1"}})
	lead.submit(g+"/resources/new", g+"/resources", url.Values{"subject_id": {id}, "kind": {"recording"}, "title": {"Lab walkthrough"}, "url": {"https://youtu.be/lab"}, "lesson_type": {"lab"}})
	lead.submit(g+"/resources/new", g+"/resources", url.Values{"subject_id": {id}, "kind": {"recording"}, "title": {"Lecture 1"}, "url": {"https://youtu.be/lk"}, "lesson_type": {"lecture"}})

	code, body, _ := stud.get(page)
	if code != http.StatusOK {
		t.Fatalf("subject page: %d", code)
	}
	for _, want := range []string{
		"<h1>Physics</h1>", ">PH<", "Bondarenko O. V.", `href="https://dl.nure.ua/course/1"`, "Attendance in DL",
		`href="https://meet.example/lab"`, "Lab 1", `aria-current="page">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("subject page lacks %q", want)
		}
	}
	if strings.Contains(body, "Lecture 1") || strings.Contains(body, "/edit") {
		t.Error("the homework tab shows recordings or a student sees the edit link")
	}

	// Recordings, filtered by lesson type.
	_, body, _ = stud.get(page + "?tab=recordings")
	if !strings.Contains(body, "Lab walkthrough") || !strings.Contains(body, "Lecture 1") {
		t.Fatalf("recordings tab:\n%s", body)
	}
	_, body, _ = stud.get(page + "?tab=recordings&lesson=lecture")
	if strings.Contains(body, "Lab walkthrough") || !strings.Contains(body, "Lecture 1") {
		t.Fatal("the lecture filter should hide the lab recording")
	}
	if _, body, _ = stud.get(page + "?tab=recordings&lesson=nope"); !strings.Contains(body, "Lab walkthrough") {
		t.Fatal("an unknown filter should show everything")
	}

	// Toggling a status without JavaScript comes back to the subject page.
	_, body, _ = stud.get(page)
	m := regexp.MustCompile(`action="` + g + `/homework/(\d+)/progress"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no status toggle on the subject page")
	}
	code, _, h := stud.submit(page, g+"/homework/"+m[1]+"/progress", url.Values{"status": {"done"}, "from": {"subject"}})
	if code != http.StatusSeeOther || h.Get("Location") != page {
		t.Fatalf("toggle from the subject page: %d %s", code, h.Get("Location"))
	}

	// Outsiders and unknown subjects.
	if code, _, _ := e.browser(t).get(page); code != http.StatusSeeOther {
		t.Fatalf("anonymous: %d", code)
	}
	if code, _, _ := stud.get(g + "/subjects/99999"); code != http.StatusNotFound {
		t.Fatalf("unknown subject: %d", code)
	}
	if code, _, _ := stud.get(g + "/subjects/abc"); code != http.StatusNotFound {
		t.Fatalf("bad id: %d", code)
	}
}
