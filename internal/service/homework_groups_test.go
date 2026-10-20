package service_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestStartOfWeek(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}
	monday := time.Date(2026, 10, 19, 0, 0, 0, 0, kyiv)
	for _, at := range []time.Time{
		monday,
		time.Date(2026, 10, 21, 12, 0, 0, 0, kyiv),
		time.Date(2026, 10, 25, 23, 59, 0, 0, kyiv),     // Sunday, after the clocks went back
		time.Date(2026, 10, 18, 21, 30, 0, 0, time.UTC), // Monday 00:30 in Kyiv
	} {
		if got := service.StartOfWeek(at, kyiv); !got.Equal(monday) {
			t.Errorf("StartOfWeek(%s) = %s, want %s", at, got, monday)
		}
	}
}

func TestGroupHomework(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, kyiv) // Wednesday
	hw := func(title string, due *time.Time, st store.ProgressStatus) *service.Homework {
		h := &service.Homework{Homework: store.Homework{Title: title, DueAt: due}, Tracked: true, Status: st}
		h.Overdue = due != nil && due.Before(now) && st != store.StatusDone
		return h
	}
	at := func(day, hour, minute int) *time.Time {
		t := time.Date(2026, 10, day, hour, minute, 0, 0, kyiv)
		return &t
	}
	list := []*service.Homework{
		hw("late", at(19, 9, 30), store.StatusInProgress),
		hw("handed in", at(20, 9, 30), store.StatusDone),
		hw("friday", at(23, 23, 59), store.StatusNotStarted),
		// The week ends at Monday midnight in Kyiv, after the clocks went back.
		hw("sunday night", at(25, 23, 59), store.StatusNotStarted),
		hw("next monday", at(26, 0, 0), store.StatusNotStarted),
		hw("someday", nil, store.StatusNotStarted),
	}
	got := service.GroupHomework(list, now, kyiv)
	titles := func(items []*service.Homework) []string {
		var out []string
		for _, h := range items {
			out = append(out, h.Title)
		}
		return out
	}
	want := map[string][]string{
		"overdue": {"late"},
		"week":    {"friday", "sunday night"},
		"later":   {"next monday"},
		"none":    {"someday"},
	}
	var keys []string
	for _, g := range got.Groups {
		keys = append(keys, g.Key)
		if !slices.Equal(titles(g.Items), want[g.Key]) {
			t.Errorf("%s: %v, want %v", g.Key, titles(g.Items), want[g.Key])
		}
	}
	if !slices.Equal(keys, []string{"overdue", "week", "later", "none"}) {
		t.Errorf("groups %v", keys)
	}
	if !slices.Equal(titles(got.Done), []string{"handed in"}) || got.Overdue != 1 || got.ThisWeek != 2 {
		t.Errorf("done %v, overdue %d, this week %d", titles(got.Done), got.Overdue, got.ThisWeek)
	}

	// Empty groups are left out.
	if got := service.GroupHomework(list[4:5], now, kyiv); len(got.Groups) != 1 || got.Groups[0].Key != "later" {
		t.Errorf("one later item: %+v", got.Groups)
	}
}

// HomeworkDue counts without reading the assignments; it must agree with
// the Overdue and This week groups of the homework page.
func TestHomeworkDue(t *testing.T) {
	f := setup(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) // Wednesday; the site's zone is UTC
	service.SetClock(f.svc, func() time.Time { return now })
	lead := f.leader(t, "lead")
	stud := f.as(t, f.register(t, "stud"))
	gid := f.group.ID
	sub := f.subject(t, lead, "Physics").ID
	day := func(d int) *time.Time {
		t := time.Date(2026, 10, d, 9, 30, 0, 0, time.UTC)
		return &t
	}
	f.homework(t, lead, sub, "overdue", nil, day(19))
	friday := f.homework(t, lead, sub, "friday", nil, day(23))
	done := f.homework(t, lead, sub, "done", nil, day(22))
	lateDone := f.homework(t, lead, sub, "late but done", nil, day(20))
	f.homework(t, lead, sub, "next week", nil, day(27))
	f.homework(t, lead, sub, "no deadline", nil, nil)
	for _, id := range []int64{done.ID, lateDone.ID} {
		if _, err := f.svc.UpdateProgress(stud, gid, id, service.ProgressInput{Status: status(store.StatusDone)}); err != nil {
			t.Fatal(err)
		}
	}
	// Someone else's progress does not count for the student.
	if _, err := f.svc.UpdateProgress(lead, gid, friday.ID, service.ProgressInput{Status: status(store.StatusDone)}); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct {
		ctx  context.Context
		want int
	}{
		"student": {stud, 2},
		"leader":  {lead, 3},
	} {
		got, err := f.svc.HomeworkDue(c.ctx, gid)
		if err != nil || got != c.want {
			t.Errorf("%s: HomeworkDue = %d, %v; want %d", name, got, err, c.want)
		}
		list, err := f.svc.HomeworkList(c.ctx, gid, service.HomeworkFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if g := service.GroupHomework(list, now, time.UTC); g.Overdue+g.ThisWeek != got {
			t.Errorf("%s: the page shows %d overdue and %d this week, the count is %d", name, g.Overdue, g.ThisWeek, got)
		}
	}

	// A superadmin who is not a member keeps no tracker: no count.
	ctx := context.Background()
	if err := f.svc.AdminCreateSuperadmin(ctx, "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Login(ctx, service.LoginInput{Username: "root", Password: "correct horse", ClientIP: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.svc.HomeworkDue(f.as(t, sess), gid); err != nil || got != 0 {
		t.Errorf("superadmin: %d, %v", got, err)
	}
	// Visitors are turned away like everywhere else.
	if _, err := f.svc.HomeworkDue(ctx, gid); err == nil {
		t.Error("visitor: want an error")
	}
}
