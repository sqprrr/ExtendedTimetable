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
		list, err := f.svc.HomeworkList(ctx, gid, service.HomeworkFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if list[0].Status != store.StatusNotStarted || list[0].Grade != nil {
			t.Errorf("%s list sees %s / %v", name, list[0].Status, list[0].Grade)
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
	olga := f.as(t, f.registerInto(t, "OTHER-1", "olga"))
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
	if err != nil || len(ov.Homework) != 0 {
		t.Fatalf("done homework on the overview: %v %+v", err, ov.Homework)
	}
	// Someone else's done does not hide it from the leader.
	if ov, err := f.svc.GroupOverview(lead, gid); err != nil || len(ov.Homework) != 1 || !ov.Homework[0].Overdue {
		t.Fatalf("leader's overview: %v %+v", err, ov.Homework)
	}

	if err := f.svc.DeleteHomework(lead, gid, hw.ID); err != nil {
		t.Fatalf("delete homework with progress: %v", err)
	}
	if list, err := f.svc.HomeworkList(stud, gid, service.HomeworkFilter{}); err != nil || len(list) != 0 {
		t.Fatalf("homework after delete: %v %+v", err, list)
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

func TestHomeworkFilter(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	stud := f.as(t, f.register(t, "stud"))
	gid := f.group.ID
	phys := f.subject(t, lead, "Physics").ID
	math := f.subject(t, lead, "Maths").ID
	lab := f.homework(t, lead, phys, "Lab", nil, nil)
	f.homework(t, lead, phys, "Essay", nil, nil)
	sums := f.homework(t, lead, math, "Sums", nil, nil)
	for id, st := range map[int64]store.ProgressStatus{lab.ID: store.StatusDone, sums.ID: store.StatusInProgress} {
		if _, err := f.svc.UpdateProgress(stud, gid, id, service.ProgressInput{Status: status(st)}); err != nil {
			t.Fatal(err)
		}
	}

	titles := func(ctx context.Context, flt service.HomeworkFilter) string {
		t.Helper()
		list, err := f.svc.HomeworkList(ctx, gid, flt)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, h := range list {
			out = append(out, h.Title)
		}
		return strings.Join(out, ",")
	}
	for _, c := range []struct {
		flt  service.HomeworkFilter
		want string
	}{
		{service.HomeworkFilter{}, "Lab,Essay,Sums"},
		{service.HomeworkFilter{SubjectID: phys}, "Lab,Essay"},
		{service.HomeworkFilter{Status: store.StatusNotStarted}, "Essay"},
		{service.HomeworkFilter{Status: store.StatusInProgress}, "Sums"},
		{service.HomeworkFilter{SubjectID: phys, Status: store.StatusDone}, "Lab"},
		{service.HomeworkFilter{SubjectID: math, Status: store.StatusDone}, ""},
	} {
		if got := titles(stud, c.flt); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.flt, got, c.want)
		}
	}
	// The status filter uses the viewer's own tracker, not the student's.
	if got := titles(lead, service.HomeworkFilter{Status: store.StatusNotStarted}); got != "Lab,Essay,Sums" {
		t.Errorf("leader's not started: %q", got)
	}
	if _, err := f.svc.HomeworkList(stud, gid, service.HomeworkFilter{Status: "finished"}); err == nil {
		t.Error("unknown status should be rejected")
	}
}
