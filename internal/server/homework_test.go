package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// The homework page: grouped rows, an Add page and row menus for leaders.
func TestHomeworkPageForLeadersAndStudents(t *testing.T) {
	e := newEnv(t)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	hwURL := setupHomework(t, e, lead)
	const list = "/g/KIUKI-25-3/homework"

	_, body, _ := lead.get(list)
	for _, want := range []string{
		`href="` + list + `/new"`,                   // Add in the page head
		`<details class="menu">`,                    // row menu
		`href="` + hwURL + `/edit"`,                 // Edit in it
		`action="` + hwURL + `/delete"`,             // Delete in it
		`<h2 class="group-title">No deadline <span`, // grouped by due date
	} {
		if !strings.Contains(body, want) {
			t.Errorf("leader's list missing %s", want)
		}
	}
	if code, body, _ := lead.get(list + "/new"); code != http.StatusOK || !strings.Contains(body, `<h1>New assignment</h1>`) || !strings.Contains(body, `name="title"`) {
		t.Fatalf("new form: %d", code)
	}

	_, body, _ = stud.get(list)
	if strings.Contains(body, `<details class="menu">`) || strings.Contains(body, list+"/new") {
		t.Error("students get no menus and no Add")
	}
	if code, _, _ := stud.get(list + "/new"); code != http.StatusForbidden {
		t.Errorf("student new form: %d", code)
	}

	// Done assignments go to the collapsed Done group.
	stud.submit(list, hwURL+"/progress", map[string][]string{"view": {"item"}, "from": {"list"}, "status": {"done"}})
	_, body, _ = stud.get(list)
	if !strings.Contains(body, `<details class="group">`) || !strings.Contains(body, "Done <span") {
		t.Error("done assignments should be in the collapsed Done group")
	}
	if _, body, _ = stud.get(list + "?status=done"); !strings.Contains(body, `<details class="group" open>`) {
		t.Error("the done filter should open the Done group")
	}
}
