package service_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// loginDemo logs in as a demo account with the passwords ResetDemo gave.
func (f *fixture) loginDemo(t *testing.T, passwords map[string]string, username string) context.Context {
	t.Helper()
	sess, err := f.svc.Login(context.Background(), service.LoginInput{Username: username, Password: passwords[username], ClientIP: "d"})
	if err != nil {
		t.Fatal(err)
	}
	return f.as(t, sess)
}

func TestResetDemo(t *testing.T) {
	f := setupWith(t, service.Config{DemoGroup: "KIUKI-25-3"})
	ctx := context.Background()
	passwords, err := f.svc.ResetDemo(ctx, "kiuki-25-3")
	if err != nil || len(passwords) != 2 || len(passwords[service.DemoUsername]) != 12 || passwords[service.DemoUsername] == passwords[service.DemoStudentUsername] {
		t.Fatalf("first reset: %v %v", err, passwords)
	}
	demo := f.loginDemo(t, passwords, service.DemoUsername)
	student := f.loginDemo(t, passwords, service.DemoStudentUsername)
	for ctx, want := range map[context.Context]store.Role{demo: store.RoleLeader, student: store.RoleStudent} {
		v := service.ViewerFrom(ctx)
		if role, ok := v.RoleIn(f.group.ID); !v.IsDemo || !ok || role != want {
			t.Fatalf("%s: demo %v, role %v %v", v.Username, v.IsDemo, role, ok)
		}
	}

	count := func() (hw, notes, links, subjects int) {
		t.Helper()
		h, err := f.svc.HomeworkList(demo, f.group.ID, service.HomeworkFilter{})
		if err != nil {
			t.Fatal(err)
		}
		n, _ := f.svc.Notes(demo, f.group.ID)
		l, _ := f.svc.ClassLinks(demo, f.group.ID)
		s, _ := f.svc.Subjects(demo, f.group.ID)
		return len(h), len(n), len(l), len(s)
	}
	hw, notes, links, subjects := count()
	if hw != 7 || notes != 3 || links != 4 || subjects != 4 {
		t.Fatalf("content: %d homework, %d notes, %d links, %d subjects", hw, notes, links, subjects)
	}
	for _, ctx := range []context.Context{demo, student} {
		list, _ := f.svc.HomeworkList(ctx, f.group.ID, service.HomeworkFilter{})
		statuses := map[store.ProgressStatus]int{}
		for _, h := range list {
			statuses[h.Status]++
		}
		if statuses[store.StatusDone] != 1 || statuses[store.StatusInProgress] != 1 {
			t.Fatalf("demo progress: %v", statuses)
		}
	}
	list, _ := f.svc.HomeworkList(demo, f.group.ID, service.HomeworkFilter{})

	// What visitors do is undone by the next reset, and nothing is doubled.
	if _, err := f.svc.UpdateProgress(demo, f.group.ID, list[len(list)-1].ID, service.ProgressInput{Status: ptr(store.StatusDone)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateNote(demo, f.group.ID, service.NoteInput{Title: "spam", Body: "visit my site"}); err != nil {
		t.Fatalf("the demo leader should edit the group: %v", err)
	}
	if _, err := f.svc.CreateNote(student, f.group.ID, service.NoteInput{Title: "spam", Body: "x"}); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("the demo student should not edit the group: %v", err)
	}
	// The leader may make the student an editor, until the night.
	studentID := service.ViewerFrom(student).UserID
	if err := f.svc.GrantEditor(demo, f.group.ID, studentID); err != nil {
		t.Fatalf("make the demo student an editor: %v", err)
	}
	if err := f.svc.RemoveMember(demo, f.group.ID, studentID); !errors.Is(err, service.ErrDemo) {
		t.Fatalf("remove the demo student: %v", err)
	}
	// The passwords stay: they are what the superadmin gave out.
	if again, err := f.svc.ResetDemo(ctx, "KIUKI-25-3"); err != nil || len(again) != 0 {
		t.Fatalf("second reset: %v %v", err, again)
	}
	f.loginDemo(t, passwords, service.DemoUsername)
	if v := service.ViewerFrom(f.loginDemo(t, passwords, service.DemoStudentUsername)); v.Memberships[0].Role != store.RoleStudent {
		t.Fatalf("the demo student should be a student again: %v", v.Memberships)
	}
	if hw, notes, links, subjects := count(); hw != 7 || notes != 3 || links != 4 || subjects != 4 {
		t.Fatalf("after a second reset: %d %d %d %d", hw, notes, links, subjects)
	}
	list, _ = f.svc.HomeworkList(demo, f.group.ID, service.HomeworkFilter{})
	done := 0
	for _, h := range list {
		if h.Status == store.StatusDone {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("visitors' progress should be undone: %d done", done)
	}
}

func TestDemoAccountLimits(t *testing.T) {
	f := setupWith(t, service.Config{DemoGroup: "KIUKI-25-3"})
	passwords, err := f.svc.ResetDemo(context.Background(), "KIUKI-25-3")
	if err != nil {
		t.Fatal(err)
	}
	demo := f.loginDemo(t, passwords, service.DemoUsername)
	if err := f.svc.LeaveGroup(f.loginDemo(t, passwords, service.DemoStudentUsername), f.group.ID); !errors.Is(err, service.ErrDemo) {
		t.Errorf("the student leaves: %v", err)
	}
	if err := f.svc.ResignLeadership(demo, f.group.ID); !errors.Is(err, service.ErrDemo) {
		t.Errorf("resign: %v", err)
	}
	if err := f.svc.LeaveGroup(demo, f.group.ID); !errors.Is(err, service.ErrDemo) {
		t.Errorf("leave: %v", err)
	}
	other, err := f.svc.AdminCreateGroup(context.Background(), "OTHER", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.JoinGroup(demo, f.invite(t, other.Code)); !errors.Is(err, service.ErrDemo) {
		t.Errorf("join another group: %v", err)
	}
	// Settings stay in the visitor's browser, not in the shared account.
	if err := f.svc.SetLocale(demo, "en"); err != nil {
		t.Fatal(err)
	}
	if v := service.ViewerFrom(f.loginDemo(t, passwords, service.DemoUsername)); v.Locale != "" {
		t.Errorf("the demo account's language was saved: %q", v.Locale)
	}
	// Nobody joins the demo group, even with its link.
	alice := f.as(t, f.registerInto(t, other.Code, "alice"))
	if err := f.svc.LeaveGroup(alice, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.JoinGroup(alice, f.invite(t, f.group.Code)); !errors.Is(err, service.ErrInviteInvalid) {
		t.Errorf("join the demo group: %v", err)
	}
	// Nobody can register as a demo account.
	for _, name := range []string{"Demo", "demo-student"} {
		_, err = f.svc.Register(context.Background(), service.RegisterInput{Username: name, Password: "correct horse",
			InviteToken: f.invite(t, other.Code), ClientIP: "r-" + name})
		if !errors.Is(err, service.ErrUsernameTaken) {
			t.Errorf("register as %s: %v", name, err)
		}
	}
}

func TestResetDemoLeavesRealAccountsAlone(t *testing.T) {
	// A site without a demo has no reserved name: someone may be "demo".
	f := setup(t)
	f.register(t, service.DemoUsername)
	other, err := f.svc.AdminCreateGroup(context.Background(), "DEMO", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ResetDemo(context.Background(), other.Code); err == nil {
		t.Fatal("a real account named demo in another group should not be taken over")
	}
	if _, err := f.svc.Login(context.Background(), service.LoginInput{Username: service.DemoUsername, Password: "correct horse", ClientIP: "x"}); err != nil {
		t.Fatalf("the real account's password should be unchanged: %v", err)
	}
	if _, err := f.svc.ResetDemo(context.Background(), "NOPE"); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("missing group: %v", err)
	}
}

func TestNextDemoReset(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ now, want time.Time }{
		{time.Date(2026, 10, 17, 3, 59, 0, 0, kyiv), time.Date(2026, 10, 17, 4, 0, 0, 0, kyiv)},
		{time.Date(2026, 10, 17, 4, 0, 0, 0, kyiv), time.Date(2026, 10, 18, 4, 0, 0, 0, kyiv)},
		{time.Date(2026, 10, 17, 23, 30, 0, 0, kyiv), time.Date(2026, 10, 18, 4, 0, 0, 0, kyiv)},
		// The night the clocks go back: still 04:00 local time.
		{time.Date(2026, 10, 24, 12, 0, 0, 0, kyiv), time.Date(2026, 10, 25, 4, 0, 0, 0, kyiv)},
	} {
		if got := service.NextDemoReset(c.now.UTC(), kyiv); !got.Equal(c.want) {
			t.Errorf("%v: %v, want %v", c.now, got, c.want)
		}
	}
}

// A demo group linked to CIST gets the subjects of its timetable back after
// a visitor deleted them, and the demo's content goes onto those.
func TestResetDemoWithTimetable(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeCIST{events: []cist.Event{
		{Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), Subject: "ООП", Type: "Лк", Room: "DL", Groups: "ДЕМО"},
		{Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour), Subject: "ВМ", Type: "Пз", Room: "285", Groups: "ДЕМО"},
	}}
	f := setupWith(t, service.Config{CIST: src, DemoGroup: "DEMO"})
	cistID := int64(42)
	g, err := f.svc.AdminCreateGroup(context.Background(), "DEMO", "", &cistID)
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := f.svc.ResetDemo(context.Background(), "DEMO")
	if err != nil {
		t.Fatal(err)
	}
	demo := f.loginDemo(t, passwords, service.DemoUsername)
	names := func() []string {
		t.Helper()
		subjects, err := f.svc.Subjects(demo, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range subjects {
			out = append(out, s.ShortName)
		}
		return out
	}
	if got := names(); len(got) != 2 {
		t.Fatalf("subjects from CIST: %v", got)
	}
	list, _ := f.svc.HomeworkList(demo, g.ID, service.HomeworkFilter{})
	for _, h := range list {
		if h.SubjectName != "ООП" && h.SubjectName != "ВМ" {
			t.Fatalf("demo homework on a subject not from CIST: %q", h.SubjectName)
		}
	}
	if _, err := f.svc.ResetDemo(context.Background(), "DEMO"); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 2 {
		t.Fatalf("subjects after a second reset: %v", got)
	}
}

// A real group must not be wiped: a mistyped EXTT_DEMO_GROUP, or
// `extt admin demo-reset` given the wrong code.
func TestResetDemoRefusesARealGroup(t *testing.T) {
	f := setup(t)
	alice := f.as(t, f.register(t, "alice"))
	root := f.superadmin(t)
	if _, err := f.svc.CreateNote(root, f.group.ID, service.NoteInput{Title: "Real note", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ResetDemo(context.Background(), "KIUKI-25-3"); !errors.Is(err, service.ErrNotDemoGroup) {
		t.Fatalf("reset of a real group: %v", err)
	}
	if notes, _ := f.svc.Notes(alice, f.group.ID); len(notes) != 1 {
		t.Fatalf("the group's content was touched: %d notes", len(notes))
	}
	if _, err := f.svc.Login(context.Background(), service.LoginInput{Username: service.DemoUsername, Password: "x", ClientIP: "z"}); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("no demo account should have been made: %v", err)
	}
}

// At start the scheduled sync may already be syncing the demo group when the
// reset clears it; the reset waits for it and syncs again, so the group ends
// with its CIST subjects only.
func TestResetDemoWaitsForARunningSync(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeCIST{events: []cist.Event{
		{Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), Subject: "ООП", Type: "Лк", Room: "DL", Groups: "ДЕМО"},
		{Start: now.Add(3 * time.Hour), End: now.Add(4 * time.Hour), Subject: "ВМ", Type: "Пз", Room: "285", Groups: "ДЕМО"},
	}, started: make(chan struct{}, 4), block: make(chan struct{})}
	f := setupWith(t, service.Config{CIST: src, DemoGroup: "DEMO"})
	cistID := int64(42)
	g, err := f.svc.AdminCreateGroup(context.Background(), "DEMO", "", &cistID)
	if err != nil {
		t.Fatal(err)
	}
	synced := make(chan error, 1)
	go func() {
		_, err := f.svc.AdminSyncSchedule(context.Background(), "DEMO", false)
		synced <- err
	}()
	<-src.started // the scheduled sync is fetching
	reset := make(chan error, 1)
	var passwords map[string]string
	go func() {
		var err error
		passwords, err = f.svc.ResetDemo(context.Background(), "DEMO")
		reset <- err
	}()
	time.Sleep(200 * time.Millisecond) // the reset has cleared the group and waits
	close(src.block)
	if err := <-synced; err != nil {
		t.Fatal(err)
	}
	if err := <-reset; err != nil {
		t.Fatal(err)
	}
	subjects, err := f.svc.Subjects(f.loginDemo(t, passwords, service.DemoUsername), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subjects) != 2 {
		var names []string
		for _, s := range subjects {
			names = append(names, s.ShortName)
		}
		t.Fatalf("want the 2 CIST subjects only, got %v", names)
	}
}

// The demo group's code is read the same everywhere, spaces or case aside.
func TestDemoGroupCodeIsNormalized(t *testing.T) {
	f := setupWith(t, service.Config{DemoGroup: " demo "})
	if _, err := f.svc.AdminCreateGroup(context.Background(), "DEMO", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ResetDemo(context.Background(), "DEMO"); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.Register(context.Background(), service.RegisterInput{Username: "mallory", Password: "correct horse",
		InviteToken: f.invite(t, "DEMO"), ClientIP: "m"})
	if !errors.Is(err, service.ErrInviteInvalid) {
		t.Fatalf("joining the demo group by its link: %v", err)
	}
}

// Anyone can try the demo accounts' public names, but not lock the demo:
// the limit per username does not apply to them.
func TestDemoLoginCannotBeLockedOut(t *testing.T) {
	f := setupWith(t, service.Config{DemoGroup: "KIUKI-25-3"})
	passwords, err := f.svc.ResetDemo(context.Background(), "KIUKI-25-3")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 110 {
		_, err := f.svc.Login(context.Background(), service.LoginInput{Username: service.DemoUsername, Password: "guess",
			ClientIP: "10.1." + strconv.Itoa(i/250) + "." + strconv.Itoa(i%250)})
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	f.loginDemo(t, passwords, service.DemoUsername)
}

func TestDemoCannotAddPlaylists(t *testing.T) {
	f := setupWith(t, service.Config{DemoGroup: "KIUKI-25-3"})
	passwords, err := f.svc.ResetDemo(context.Background(), "KIUKI-25-3")
	if err != nil {
		t.Fatal(err)
	}
	demo := f.loginDemo(t, passwords, service.DemoUsername)
	if err := f.svc.AddPlaylist(demo, f.group.ID, "https://www.youtube.com/playlist?list=PLNlrZymF62fMabcdefghijklmnopqrstu"); !errors.Is(err, service.ErrDemo) {
		t.Fatalf("add a playlist: %v", err)
	}
	if src, err := f.svc.RecordingsSourceFor(demo, f.group.ID); err != nil || src.CanAdd || !src.Demo {
		t.Fatalf("recordings source: %v %+v", err, src)
	}
}
