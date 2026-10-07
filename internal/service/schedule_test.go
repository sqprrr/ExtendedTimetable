package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

// fakeCIST serves canned events and records what was asked.
type fakeCIST struct {
	mu     sync.Mutex
	events []cist.Event
	err    error
	calls  int
	// When block is set, a fetch signals started and waits for block.
	started chan struct{}
	block   chan struct{}
}

func (f *fakeCIST) GroupEvents(_ context.Context, id int64, from, to time.Time) ([]cist.Event, error) {
	f.mu.Lock()
	started, block := f.started, f.block
	f.mu.Unlock()
	if block != nil {
		started <- struct{}{}
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []cist.Event
	for _, e := range f.events {
		if !e.Start.Before(from) && e.Start.Before(to) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeCIST) set(events []cist.Event, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events, f.err = events, err
}

// scheduleFixture is a group with CIST id 42, a leader and a student, and a
// service whose clock the test controls.
type scheduleFixture struct {
	*fixture
	cist  *fakeCIST
	now   *time.Time
	lead  context.Context
	stud  context.Context
	kyiv  *time.Location
	start time.Time // Monday 00:00 of the test week, Kyiv time
}

func setupSchedule(t *testing.T) *scheduleFixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}
	src := &fakeCIST{}
	// Wednesday 7 October 2026, 12:00 in Kyiv.
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, kyiv)
	svc := service.New(st, service.Config{CIST: src, Location: kyiv})
	service.SetClock(svc, func() time.Time { return now })
	cistID := int64(42)
	g, err := svc.AdminCreateGroup(ctx, "KIUKI-25-3", "", &cistID)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{svc: svc, group: g}
	sf := &scheduleFixture{fixture: f, cist: src, now: &now, kyiv: kyiv,
		start: time.Date(2026, 10, 5, 0, 0, 0, 0, kyiv)}
	sf.lead = f.leader(t, "lead")
	sf.stud = f.as(t, f.register(t, "stud"))
	return sf
}

// at is a class on day d (0 = Monday) of the test week at hh:mm, 95 minutes long.
func (f *scheduleFixture) at(d, hh, mm int, subject, typ string) cist.Event {
	start := f.start.AddDate(0, 0, d).Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute)
	return cist.Event{Start: start, End: start.Add(95 * time.Minute), Subject: subject, Type: typ, Room: "DL", Groups: "КІУКІ-25-3"}
}

func (f *scheduleFixture) week(t *testing.T, ctx context.Context) *service.Schedule {
	t.Helper()
	sch, err := f.svc.Schedule(ctx, f.group.ID, f.start, f.start.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func TestSyncCreatesSubjectsAndLinksClassLinks(t *testing.T) {
	f := setupSchedule(t)
	gid := f.group.ID
	// The leader already has one subject whose short name matches CIST.
	oop, err := f.svc.CreateSubject(f.lead, gid, service.SubjectInput{Name: "Об'єктно-орієнтоване програмування", ShortName: "ооПРО"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateClassLink(f.lead, gid, service.ClassLinkInput{SubjectID: oop.ID, LessonType: store.LessonLecture, URL: "https://meet.example/oop"}); err != nil {
		t.Fatal(err)
	}
	f.cist.set([]cist.Event{
		f.at(0, 9, 30, "ІМ", "Пз"),
		f.at(2, 11, 15, "ООПро", "Лк"), // in progress at 12:00 on Wednesday
		f.at(2, 13, 10, "ООПро", "Пз"),
		f.at(3, 11, 15, "МОАП", "Лб"),
		f.at(4, 9, 30, "ВМ", "Екз"),
	}, nil)

	rec, err := f.svc.SyncScheduleNow(f.lead, gid)
	if err != nil {
		t.Fatal(err)
	}
	if rec.EventCount != 5 || rec.LastError != "" || rec.LastSuccessAt == nil {
		t.Fatalf("sync record: %+v", rec)
	}

	// Missing subjects were created from the CIST short names; the existing
	// one was matched ignoring case, not duplicated.
	subjects, err := f.svc.Subjects(f.stud, gid)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, s := range subjects {
		names[s.Name] = s.ShortName
	}
	if len(subjects) != 4 || names["ІМ"] != "ІМ" || names["МОАП"] != "МОАП" || names["ВМ"] != "ВМ" || names[oop.Name] != "ооПРО" {
		t.Fatalf("subjects after sync: %v", names)
	}

	sch := f.week(t, f.stud)
	if len(sch.Events) != 5 || !sch.HasSource || sch.Sync == nil {
		t.Fatalf("week: %+v", sch)
	}
	lecture := sch.Events[1]
	if lecture.Title() != oop.Name || lecture.LessonType != store.LessonLecture || !lecture.Now ||
		len(lecture.ClassLinks) != 1 || lecture.ClassLinks[0].URL != "https://meet.example/oop" {
		t.Fatalf("lecture: %+v links=%+v", lecture, lecture.ClassLinks)
	}
	if practice := sch.Events[2]; len(practice.ClassLinks) != 0 || practice.Now {
		t.Errorf("the practice has no link of its own: %+v", practice)
	}
	if exam := sch.Events[4]; exam.LessonType != "" || exam.CISTType != "Екз" {
		t.Errorf("exams have no class link type: %+v", exam)
	}
	if len(sch.Upcoming) != 1 || sch.Upcoming[0].ID != lecture.ID || !sch.IsUpcoming(lecture) {
		t.Errorf("upcoming: %+v", sch.Upcoming)
	}

	// Today on the overview is Wednesday's two classes.
	ov, err := f.svc.GroupOverview(f.stud, gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Today.Events) != 2 || ov.Today.Events[0].SubjectBrief != "ООПро" {
		t.Errorf("today: %+v", ov.Today.Events)
	}
}

func TestSyncKeepsCacheWhenCISTFails(t *testing.T) {
	f := setupSchedule(t)
	f.cist.set([]cist.Event{f.at(0, 9, 30, "ІМ", "Пз"), f.at(1, 9, 30, "ФВ", "Пз")}, nil)
	if _, err := f.svc.AdminSyncSchedule(context.Background(), "kiuki-25-3"); err != nil {
		t.Fatal(err)
	}
	firstSuccess := *f.now

	// CIST goes down an hour later.
	*f.now = f.now.Add(time.Hour)
	f.cist.set(nil, errors.New("cist.nure.ua: timeout"))
	rec, err := f.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3")
	var serr *service.SyncError
	if !errors.As(err, &serr) || rec == nil || rec.LastError == "" || !rec.LastSuccessAt.Equal(firstSuccess) {
		t.Fatalf("failed sync: %v %+v", err, rec)
	}
	if sch := f.week(t, f.stud); len(sch.Events) != 2 || sch.Sync.LastError == "" {
		t.Fatalf("the cached copy should stay: %+v", sch)
	}

	// A good sync replaces the window and clears the error.
	f.cist.set([]cist.Event{f.at(2, 9, 30, "ІМ", "Пз")}, nil)
	rec, err = f.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3")
	if err != nil || rec.LastError != "" || rec.EventCount != 1 {
		t.Fatalf("recovery: %v %+v", err, rec)
	}
	if sch := f.week(t, f.stud); len(sch.Events) != 1 || sch.Events[0].StartsAt.In(f.kyiv).Weekday() != time.Wednesday {
		t.Fatalf("replaced week: %+v", sch.Events)
	}
}

func TestSyncPermissionsAndCooldown(t *testing.T) {
	f := setupSchedule(t)
	gid := f.group.ID
	f.cist.set([]cist.Event{f.at(0, 9, 30, "ІМ", "Пз")}, nil)

	if _, err := f.svc.SyncScheduleNow(f.stud, gid); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student sync: %v", err)
	}
	if _, err := f.svc.SyncScheduleNow(f.lead, gid); err != nil {
		t.Fatal(err)
	}
	*f.now = f.now.Add(time.Minute)
	if _, err := f.svc.SyncScheduleNow(f.lead, gid); !errors.Is(err, service.ErrSyncTooSoon) {
		t.Fatalf("second sync within the cooldown: %v", err)
	}
	*f.now = f.now.Add(5 * time.Minute)
	if _, err := f.svc.SyncScheduleNow(f.lead, gid); err != nil {
		t.Fatalf("after the cooldown: %v", err)
	}
	if f.cist.calls != 2 {
		t.Errorf("CIST calls = %d, want 2", f.cist.calls)
	}

	// A group without a CIST id cannot sync but still shows an empty schedule.
	if err := f.svc.AdminSetCISTID(context.Background(), "KIUKI-25-3", nil); err != nil {
		t.Fatal(err)
	}
	*f.now = f.now.Add(time.Hour)
	if _, err := f.svc.SyncScheduleNow(f.lead, gid); !errors.Is(err, service.ErrNoCISTGroup) {
		t.Fatalf("no CIST id: %v", err)
	}
	if sch := f.week(t, f.stud); sch.HasSource {
		t.Error("HasSource should be false without a CIST id")
	}
}

func TestScheduleRangeAndAccess(t *testing.T) {
	f := setupSchedule(t)
	gid := f.group.ID
	_, err := f.svc.Schedule(f.stud, gid, f.start, f.start.AddDate(0, 3, 0))
	wantInputError(t, err, "to")
	if _, err := f.svc.Schedule(context.Background(), gid, f.start, f.start.AddDate(0, 0, 7)); !errors.Is(err, service.ErrUnauthenticated) {
		t.Fatalf("anonymous: %v", err)
	}
	if _, err := f.svc.AdminSyncSchedule(context.Background(), "NOPE"); err == nil {
		t.Fatal("unknown group should fail")
	}
}

func TestSyncDisabledWithoutSource(t *testing.T) {
	f := setup(t) // no CIST source
	cistID := int64(1)
	if err := f.svc.AdminSetCISTID(context.Background(), f.group.Code, &cistID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AdminSyncSchedule(context.Background(), f.group.Code); !errors.Is(err, service.ErrSyncDisabled) {
		t.Fatalf("got %v", err)
	}
}

// A valid export with no classes (holidays) is a success and empties the
// window, so classes CIST cancelled do not linger.
func TestEmptyExportClearsWindow(t *testing.T) {
	f := setupSchedule(t)
	f.cist.set([]cist.Event{f.at(3, 9, 30, "ІМ", "Пз")}, nil)
	if _, err := f.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3"); err != nil {
		t.Fatal(err)
	}
	f.cist.set(nil, nil)
	rec, err := f.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3")
	if err != nil || rec.LastError != "" || rec.EventCount != 0 {
		t.Fatalf("empty export: %v %+v", err, rec)
	}
	if sch := f.week(t, f.stud); len(sch.Events) != 0 {
		t.Fatalf("cancelled class still shown: %+v", sch.Events)
	}
}

func TestLeaderCanRenameAndDeleteSyncedSubjects(t *testing.T) {
	f := setupSchedule(t)
	ctx := context.Background()
	gid := f.group.ID
	f.cist.set([]cist.Event{f.at(0, 9, 30, "ІМ", "Пз"), f.at(1, 9, 30, "ФВ", "Пз")}, nil)
	if _, err := f.svc.AdminSyncSchedule(ctx, "KIUKI-25-3"); err != nil {
		t.Fatal(err)
	}
	subjects, _ := f.svc.Subjects(f.lead, gid)
	byName := map[string]int64{}
	for _, s := range subjects {
		byName[s.Name] = s.ID
	}

	// Rename ІМ completely, short name included; delete ФВ.
	if _, err := f.svc.UpdateSubject(f.lead, gid, byName["ІМ"], service.SubjectInput{Name: "Іноземна мова", ShortName: "Англ"}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteSubject(f.lead, gid, byName["ФВ"]); err != nil {
		t.Fatalf("a subject used only by the schedule should be deletable: %v", err)
	}

	*f.now = f.now.Add(time.Hour)
	if _, err := f.svc.AdminSyncSchedule(ctx, "KIUKI-25-3"); err != nil {
		t.Fatal(err)
	}
	subjects, _ = f.svc.Subjects(f.lead, gid)
	if len(subjects) != 1 || subjects[0].Name != "Іноземна мова" {
		t.Fatalf("the sync should neither duplicate a renamed subject nor bring back a deleted one: %+v", subjects)
	}
	sch := f.week(t, f.stud)
	if sch.Events[0].Title() != "Іноземна мова" || sch.Events[1].SubjectID != nil || sch.Events[1].Title() != "ФВ" {
		t.Fatalf("events after rename and delete: %q %v %q", sch.Events[0].Title(), sch.Events[1].SubjectID, sch.Events[1].Title())
	}

	// A new short name still gets a subject.
	f.cist.set([]cist.Event{f.at(0, 9, 30, "ІМ", "Пз"), f.at(2, 9, 30, "КЕ", "Лк")}, nil)
	*f.now = f.now.Add(time.Hour)
	if _, err := f.svc.AdminSyncSchedule(ctx, "KIUKI-25-3"); err != nil {
		t.Fatal(err)
	}
	if subjects, _ = f.svc.Subjects(f.lead, gid); len(subjects) != 2 {
		t.Fatalf("new short name: %+v", subjects)
	}
}

func TestLongShortNameSyncsRepeatedly(t *testing.T) {
	f := setupSchedule(t)
	long := strings.Repeat("Д", 120)
	f.cist.set([]cist.Event{f.at(0, 9, 30, long, "Лк")}, nil)
	for i := range 2 {
		*f.now = f.now.Add(time.Hour)
		if _, err := f.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3"); err != nil {
			t.Fatalf("sync %d: %v", i+1, err)
		}
	}
	if subjects, _ := f.svc.Subjects(f.lead, f.group.ID); len(subjects) != 1 {
		t.Fatalf("subjects: %+v", subjects)
	}
}

func TestUpcomingSkipsEndedClassOfOtherSubgroup(t *testing.T) {
	f := setupSchedule(t)
	long := f.at(3, 8, 0, "МОАП", "Лб") // 08:00–09:35
	short := f.at(3, 8, 0, "ФВ", "Пз")
	short.End = short.Start.Add(45 * time.Minute) // 08:00–08:45
	f.cist.set([]cist.Event{long, short}, nil)
	if _, err := f.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3"); err != nil {
		t.Fatal(err)
	}
	*f.now = f.start.AddDate(0, 0, 3).Add(9 * time.Hour) // Thursday 09:00
	sch := f.week(t, f.stud)
	if len(sch.Upcoming) != 1 || sch.Upcoming[0].SubjectBrief != "МОАП" || !sch.Upcoming[0].Now {
		t.Fatalf("upcoming at 09:00: %+v", sch.Upcoming)
	}
}

func TestScheduleRangeAcrossClockChange(t *testing.T) {
	f := setupSchedule(t)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, f.kyiv) // Kyiv leaves summer time on 25 October
	if _, err := f.svc.Schedule(f.stud, f.group.ID, from, from.AddDate(0, 0, 62)); err != nil {
		t.Fatalf("62 days across the clock change: %v", err)
	}
	_, err := f.svc.Schedule(f.stud, f.group.ID, from, from.AddDate(0, 0, 63))
	wantInputError(t, err, "to")
}

func TestOneSyncPerGroupAtATime(t *testing.T) {
	f := setupSchedule(t)
	f.cist.set([]cist.Event{f.at(0, 9, 30, "ІМ", "Пз")}, nil)
	f.cist.mu.Lock()
	f.cist.started, f.cist.block = make(chan struct{}), make(chan struct{})
	f.cist.mu.Unlock()

	done := make(chan error)
	go func() {
		_, err := f.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3")
		done <- err
	}()
	<-f.cist.started // the background sync is waiting on CIST
	if _, err := f.svc.SyncScheduleNow(f.lead, f.group.ID); !errors.Is(err, service.ErrSyncRunning) {
		t.Fatalf("a second sync while one runs: %v", err)
	}
	close(f.cist.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.cist.calls != 1 {
		t.Errorf("CIST calls = %d, want 1", f.cist.calls)
	}
}
