package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func ptr[T any](v T) *T { return &v }

func status(st store.ProgressStatus) *store.ProgressStatus { return &st }

// homework creates an assignment as the leader.
func (f *fixture) homework(t *testing.T, lead context.Context, subjectID int64, title string, max *float64, due *time.Time) *service.Homework {
	t.Helper()
	hw, err := f.svc.CreateHomework(lead, f.group.ID, service.HomeworkInput{SubjectID: subjectID, Title: title, MaxPoints: max, DueAt: due})
	if err != nil {
		t.Fatal(err)
	}
	return hw
}

func (f *fixture) subject(t *testing.T, lead context.Context, name string) *store.Subject {
	t.Helper()
	sub, err := f.svc.CreateSubject(lead, f.group.ID, service.SubjectInput{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestProgressIsPrivate(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	alice := f.as(t, f.register(t, "alice"))
	bob := f.as(t, f.register(t, "bob"))
	gid := f.group.ID
	hw := f.homework(t, lead, f.subject(t, lead, "Physics").ID, "Lab", ptr(10.0), nil)

	got, err := f.svc.UpdateProgress(alice, gid, hw.ID, service.ProgressInput{
		Status: status(store.StatusDone), SetGrade: true, Grade: ptr(8.5)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Tracked || got.Status != store.StatusDone || got.Grade == nil || *got.Grade != 8.5 {
		t.Fatalf("alice after update: %+v", got)
	}

	// Nobody else sees Alice's progress, the leader included.
	for name, ctx := range map[string]context.Context{"bob": bob, "lead": lead} {
		hw, err := f.svc.Homework(ctx, gid, hw.ID)
		if err != nil {
			t.Fatal(err)
		}
		if hw.Status != store.StatusNotStarted || hw.Grade != nil {
			t.Errorf("%s sees %s / %v", name, hw.Status, hw.Grade)
		}
		list, err := f.svc.HomeworkList(ctx, gid)
		if err != nil {
			t.Fatal(err)
		}
		if list[0].Status != store.StatusNotStarted || list[0].Grade != nil {
			t.Errorf("%s list sees %s / %v", name, list[0].Status, list[0].Grade)
		}
		g, err := f.svc.MyGrades(ctx, gid)
		if err != nil {
			t.Fatal(err)
		}
		if g.Overall.Earned != 0 || g.Overall.Graded != 0 {
			t.Errorf("%s grades include someone else's: %+v", name, g.Overall)
		}
	}

	// The leader keeps a tracker of their own.
	if _, err := f.svc.UpdateProgress(lead, gid, hw.ID, service.ProgressInput{Status: status(store.StatusInProgress)}); err != nil {
		t.Fatalf("leader own progress: %v", err)
	}
	if again, _ := f.svc.Homework(alice, gid, hw.ID); again.Status != store.StatusDone {
		t.Errorf("leader's update changed alice's status: %s", again.Status)
	}
}

func TestSuperadminHasNoTracker(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lead := f.leader(t, "lead")
	hw := f.homework(t, lead, f.subject(t, lead, "Physics").ID, "Lab", nil, nil)
	if err := f.svc.AdminCreateSuperadmin(ctx, "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Login(ctx, service.LoginInput{Username: "root", Password: "correct horse", ClientIP: "r"})
	if err != nil {
		t.Fatal(err)
	}
	root := f.as(t, sess)

	got, err := f.svc.Homework(root, f.group.ID, hw.ID)
	if err != nil || got.Tracked || got.Status != "" {
		t.Fatalf("superadmin view: %v %+v", err, got)
	}
	if g, _ := f.svc.Group(root, f.group.Code); g.CanTrack {
		t.Error("superadmin should not get a tracker in a group they are not in")
	}
	if _, err := f.svc.UpdateProgress(root, f.group.ID, hw.ID, service.ProgressInput{Status: status(store.StatusDone)}); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("superadmin update: %v", err)
	}
	if _, err := f.svc.MyGrades(root, f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("superadmin grades: %v", err)
	}
}

func TestProgressValidation(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	stud := f.as(t, f.register(t, "stud"))
	gid := f.group.ID
	sub := f.subject(t, lead, "Physics")
	capped := f.homework(t, lead, sub.ID, "Capped", ptr(10.0), nil)
	open := f.homework(t, lead, sub.ID, "No max", nil, nil)

	for _, tc := range []struct {
		hw    int64
		in    service.ProgressInput
		field string
		msg   string
	}{
		{capped.ID, service.ProgressInput{Status: status("finished")}, "status", ""},
		{capped.ID, service.ProgressInput{SetGrade: true, Grade: ptr(-1.0)}, "grade", ""},
		{capped.ID, service.ProgressInput{SetGrade: true, Grade: ptr(10.5)}, "grade", "more than the 10 max points"},
		{open.ID, service.ProgressInput{SetGrade: true, Grade: ptr(20000.0)}, "grade", "at most 10000"},
	} {
		_, err := f.svc.UpdateProgress(stud, gid, tc.hw, tc.in)
		wantInputError(t, err, tc.field)
		if !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%+v: message %q should mention %q", tc.in, err, tc.msg)
		}
	}

	// A grade without max points is fine; a later status change keeps it; an
	// explicit nil clears it.
	if _, err := f.svc.UpdateProgress(stud, gid, open.ID, service.ProgressInput{SetGrade: true, Grade: ptr(42.0)}); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.UpdateProgress(stud, gid, open.ID, service.ProgressInput{Status: status(store.StatusDone)})
	if err != nil || got.Grade == nil || *got.Grade != 42 {
		t.Fatalf("status change dropped the grade: %v %+v", err, got)
	}
	got, err = f.svc.UpdateProgress(stud, gid, open.ID, service.ProgressInput{SetGrade: true})
	if err != nil || got.Grade != nil || got.Status != store.StatusDone {
		t.Fatalf("clearing the grade: %v %+v", err, got)
	}

	if _, err := f.svc.UpdateProgress(stud, gid, 9999, service.ProgressInput{Status: status(store.StatusDone)}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("missing homework: %v", err)
	}
}

func TestProgressStaysInItsGroup(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lead := f.leader(t, "lead")
	hw := f.homework(t, lead, f.subject(t, lead, "Physics").ID, "Lab", nil, nil)
	if _, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "", nil); err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Register(ctx, service.RegisterInput{Username: "olga", Password: "correct horse", GroupCode: "OTHER-1", ClientIP: "o"})
	if err != nil {
		t.Fatal(err)
	}
	olga := f.as(t, sess)
	other, err := f.svc.Group(olga, "OTHER-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateProgress(olga, f.group.ID, hw.ID, service.ProgressInput{Status: status(store.StatusDone)}); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("tracking another group's homework: %v", err)
	}
	if _, err := f.svc.UpdateProgress(olga, other.ID, hw.ID, service.ProgressInput{Status: status(store.StatusDone)}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("another group's homework through one's own group: %v", err)
	}
}

func TestDoneIsNotOverdueAndDeleteCascades(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	stud := f.as(t, f.register(t, "stud"))
	gid := f.group.ID
	past := time.Now().Add(-time.Hour)
	hw := f.homework(t, lead, f.subject(t, lead, "Physics").ID, "Late", nil, &past)

	if got, _ := f.svc.Homework(stud, gid, hw.ID); !got.Overdue {
		t.Fatal("past due and not done should be overdue")
	}
	got, err := f.svc.UpdateProgress(stud, gid, hw.ID, service.ProgressInput{Status: status(store.StatusDone)})
	if err != nil || got.Overdue {
		t.Fatalf("done should not be overdue: %v %+v", err, got)
	}
	ov, err := f.svc.GroupOverview(stud, gid)
	if err != nil || len(ov.Homework) != 1 || ov.Homework[0].Status != store.StatusDone || ov.Homework[0].Overdue {
		t.Fatalf("overview: %v %+v", err, ov.Homework)
	}

	if err := f.svc.DeleteHomework(lead, gid, hw.ID); err != nil {
		t.Fatalf("delete homework with progress: %v", err)
	}
	if g, err := f.svc.MyGrades(stud, gid); err != nil || len(g.Subjects) != 0 {
		t.Fatalf("grades after delete: %v %+v", err, g)
	}
}

func TestMyGrades(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	stud := f.as(t, f.register(t, "stud"))
	gid := f.group.ID
	phys, chem := f.subject(t, lead, "Physics"), f.subject(t, lead, "Chemistry")
	p1 := f.homework(t, lead, phys.ID, "P1", ptr(10.0), nil)
	f.homework(t, lead, phys.ID, "P2", ptr(20.0), nil) // not graded yet
	p3 := f.homework(t, lead, phys.ID, "P3", nil, nil)
	c1 := f.homework(t, lead, chem.ID, "C1", ptr(5.0), nil)
	for _, g := range []struct {
		id    int64
		grade float64
	}{{p1.ID, 7}, {p3.ID, 2}, {c1.ID, 5}} {
		if _, err := f.svc.UpdateProgress(stud, gid, g.id, service.ProgressInput{SetGrade: true, Grade: ptr(g.grade)}); err != nil {
			t.Fatal(err)
		}
	}

	g, err := f.svc.MyGrades(stud, gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Subjects) != 2 || g.Subjects[0].SubjectName != "Chemistry" || g.Subjects[1].SubjectName != "Physics" {
		t.Fatalf("subjects: %+v", g.Subjects)
	}
	// P3 has no max points, so its grade is left out.
	want := map[string]service.Totals{
		"Chemistry": {Assignments: 1, Graded: 1, Earned: 5, Max: 5},
		"Physics":   {Assignments: 2, Graded: 1, Earned: 7, Max: 30},
	}
	for _, st := range g.Subjects {
		if st.Totals != want[st.SubjectName] {
			t.Errorf("%s: %+v, want %+v", st.SubjectName, st.Totals, want[st.SubjectName])
		}
	}
	if g.Overall != (service.Totals{Assignments: 3, Graded: 2, Earned: 12, Max: 35}) {
		t.Errorf("overall: %+v", g.Overall)
	}

	// A subject whose assignments have no max points is not listed.
	other := f.subject(t, lead, "Art")
	f.homework(t, lead, other.ID, "Draw", nil, nil)
	if g, _ := f.svc.MyGrades(stud, gid); len(g.Subjects) != 2 {
		t.Errorf("subjects without max points should be left out: %+v", g.Subjects)
	}
}

func TestLoweredMaxPoints(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	stud := f.as(t, f.register(t, "stud"))
	gid := f.group.ID
	sub := f.subject(t, lead, "Physics")
	hw := f.homework(t, lead, sub.ID, "Lab", ptr(10.0), nil)
	if _, err := f.svc.UpdateProgress(stud, gid, hw.ID, service.ProgressInput{SetGrade: true, Grade: ptr(9.0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateHomework(lead, gid, hw.ID, service.HomeworkInput{SubjectID: sub.ID, Title: "Lab", MaxPoints: ptr(5.0)}); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.Homework(stud, gid, hw.ID)
	if err != nil || !got.GradeOverMax || *got.Grade != 9 {
		t.Fatalf("the stored grade should be kept and flagged: %v %+v", err, got)
	}
	// Changing only the status still works.
	if got, err = f.svc.UpdateProgress(stud, gid, hw.ID, service.ProgressInput{Status: status(store.StatusDone)}); err != nil || *got.Grade != 9 {
		t.Fatalf("status change with an over-max grade: %v %+v", err, got)
	}
	// The totals count it as the max.
	g, err := f.svc.MyGrades(stud, gid)
	if err != nil || g.Overall.Earned != 5 || g.Overall.Max != 5 {
		t.Fatalf("totals: %v %+v", err, g.Overall)
	}
}

func TestNextStatusCycles(t *testing.T) {
	st := store.StatusNotStarted
	var seen []store.ProgressStatus
	for range 3 {
		st = service.NextStatus(st)
		seen = append(seen, st)
	}
	if seen[0] != store.StatusInProgress || seen[1] != store.StatusDone || seen[2] != store.StatusNotStarted {
		t.Fatalf("cycle: %v", seen)
	}
}
