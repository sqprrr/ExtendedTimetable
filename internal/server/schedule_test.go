package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/server"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/migrations"
)

type fakeCIST struct {
	mu     sync.Mutex
	events []cist.Event
	err    error
}

func (f *fakeCIST) GroupEvents(_ context.Context, _ int64, from, to time.Time) ([]cist.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func regexpOption(name string) *regexp.Regexp {
	return regexp.MustCompile(`<option value="(\d+)"[^>]*>` + regexp.QuoteMeta(name) + `</option>`)
}

// newScheduleEnv is newEnv with a fake CIST and KIUKI-25-3 linked to it.
func newScheduleEnv(t *testing.T, src *fakeCIST) *env {
	t.Helper()
	return newScheduleEnvIn(t, src, nil)
}

// newScheduleEnvIn is newScheduleEnv with the site in time zone loc.
func newScheduleEnvIn(t *testing.T, src *fakeCIST, loc *time.Location) *env {
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
	svc := service.New(st, service.Config{CIST: src, Location: loc})
	cistID := int64(11881842)
	if _, err := svc.AdminCreateGroup(ctx, "KIUKI-25-3", "", &cistID); err != nil {
		t.Fatal(err)
	}
	h, err := server.New(svc, server.Config{SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return &env{srv: srv, svc: svc}
}

func TestSchedulePages(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeCIST{events: []cist.Event{
		// In progress right now.
		{Start: now.Add(-time.Minute), End: now.Add(85 * time.Minute), Subject: "ООПро", Type: "Лк", Room: "DL", Groups: "КІУКІ-25-1,2,3"},
		{Start: now.Add(3 * time.Hour), End: now.Add(4*time.Hour + 35*time.Minute), Subject: "ВМ", Type: "Екз", Room: "287", Groups: "КІУКІ-25-3"},
	}}
	e := newScheduleEnv(t, src)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	const g = "/g/KIUKI-25-3"

	if _, body, _ := stud.get(g + "/schedule"); !strings.Contains(body, "has not been loaded from CIST yet") || strings.Contains(body, "Sync with CIST now") {
		t.Fatalf("before the first sync:\n%s", body)
	}
	if code, _, _ := stud.submit(g+"/schedule", g+"/schedule/sync", url.Values{}); code != http.StatusForbidden {
		t.Fatalf("student sync: %d", code)
	}

	// The week of the lecture (just after midnight on a Monday it started
	// the week before).
	week := src.events[0].Start.Format(time.DateOnly)
	code, _, h := lead.submit(g+"/schedule", g+"/schedule/sync", url.Values{"week": {week}})
	if code != http.StatusSeeOther || h.Get("Location") != g+"/schedule?week="+week {
		t.Fatalf("leader sync: %d %s", code, h.Get("Location"))
	}

	// The lecture links to the meeting once the leader adds a class link for
	// the subject the sync created.
	_, body, _ := lead.get(g + "/links/new")
	m := regexpOption("ООПро").FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the sync should have created subject ООПро:\n%s", body)
	}
	lead.submit(g+"/links", g+"/links", url.Values{"subject_id": {m[1]}, "lesson_type": {"lecture"}, "url": {"https://meet.example/oop"}})

	_, body, _ = stud.get(g + "/schedule?week=" + week)
	for _, want := range []string{"ООПро", "Lecture", ">Now<", "https://meet.example/oop", "Екз</span>", "287</span>", "Synced with CIST"} {
		if !strings.Contains(body, want) {
			t.Errorf("schedule page missing %q", want)
		}
	}
	if src.events[0].Start.Day() == now.Day() {
		if _, body, _ := stud.get(g); !strings.Contains(body, "<h2>Classes today</h2>") || !strings.Contains(body, "ООПро") {
			t.Errorf("overview should show today's classes:\n%s", body)
		}
	}

	// The day strip selects a day: phones and the Day view show only that
	// one; the Week view lists them all.
	day := src.events[0].Start.In(time.UTC).Format(time.DateOnly)
	_, body, _ = stud.get(g + "/schedule?week=" + week + "&day=" + day + "&view=day")
	for _, want := range []string{`class="section-gap view-day"`, `class="card day-card is-selected`, `day=` + day + `&amp;view=day" data-day="` + day + `" aria-current="date"`, `aria-current="page">Day</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("day view missing %q", want)
		}
	}
	if strings.Count(body, "is-selected") != 1 {
		t.Error("exactly one day should be selected")
	}

	// Today opens with the class in progress, how long is left and its link.
	_, body, _ = stud.get(g)
	for _, want := range []string{`class="now-card is-now"`, "Now · 85 min left", "ООПро", `<progress class="bar bar-thin"`, `href="https://meet.example/oop"`} {
		if !strings.Contains(body, want) {
			t.Errorf("Today's now card missing %q", want)
		}
	}

	// Syncing again right away is refused with a clear message.
	code, body, _ = lead.submit(g+"/schedule", g+"/schedule/sync", url.Values{})
	if code != http.StatusTooManyRequests || !strings.Contains(body, "less than 5 minutes ago") {
		t.Fatalf("second sync: %d", code)
	}

	// When CIST fails, the page keeps the old copy and says so.
	src.mu.Lock()
	src.err = errors.New("cist.nure.ua: timeout")
	src.mu.Unlock()
	if _, err := e.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3", false); err == nil {
		t.Fatal("expected the sync to fail")
	}
	_, body, _ = stud.get(g + "/schedule?week=" + week)
	if !strings.Contains(body, "failed: cist.nure.ua: timeout") || !strings.Contains(body, "Showing the last good copy") || !strings.Contains(body, "ООПро") {
		t.Fatalf("after a failed sync:\n%s", body)
	}
}

// lectureWithTeacher syncs src's classes into KIUKI-25-3, gives subject
// ООПро the lecturer Pavlenko O. V. and returns a student.
func lectureWithTeacher(t *testing.T, src *fakeCIST) *browser {
	t.Helper()
	e := newScheduleEnv(t, src)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	const g = "/g/KIUKI-25-3"
	lead.submit(g+"/schedule", g+"/schedule/sync", url.Values{"week": {src.events[0].Start.Format(time.DateOnly)}})
	_, body, _ := lead.get(g + "/links/new")
	m := regexpOption("ООПро").FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the sync should have created subject ООПро")
	}
	if code, _, _ := lead.submit(g+"/subjects/"+m[1]+"/edit", g+"/subjects/"+m[1], url.Values{"name": {"ООПро"}, "lecturer": {"Pavlenko O. V."}}); code != http.StatusSeeOther {
		t.Fatalf("set the lecturer: %d", code)
	}
	return stud
}

// The schedule's rows name the teacher and say when a class to come has no
// meeting link; on phones the week arrows move to the top bar and a line
// says when the next class after today is.
func TestSchedulePageDetails(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeCIST{events: []cist.Event{
		{Start: now.Add(-10 * time.Minute), End: now.Add(85 * time.Minute), Subject: "ООПро", Type: "Лк", Room: "DL", Groups: "КІУКІ-25-3"},
		{Start: now.Add(26 * time.Hour), End: now.Add(27 * time.Hour), Subject: "ВМ", Type: "Пз", Room: "165", Groups: "КІУКІ-25-3"},
	}}
	stud := lectureWithTeacher(t, src)
	_, body, _ := stud.get("/g/KIUKI-25-3/schedule")

	topbar := body[strings.Index(body, `<header class="topbar"`):]
	topbar = topbar[:strings.Index(topbar, "</header>")]
	if !strings.Contains(topbar, `<nav class="week-nav"`) || strings.Contains(topbar, `action="/lang"`) {
		t.Errorf("the phone top bar should hold the week arrows, not the language:\n%s", topbar)
	}
	monday := now.AddDate(0, 0, -(int(now.Weekday())+6)%7)
	if short := monday.Format("2") + "–"; !strings.Contains(topbar, `<span class="label">`+short) {
		t.Errorf("top bar: no week label starting %s", short)
	}
	for _, want := range []string{
		`<div class="toolbar hide-sm">`,
		`<span class="meta-item">Pavlenko O. V.</span>`,
		`<span class="no-link only-md">No link</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("schedule missing %s", want)
		}
	}
	tomorrow := now.Add(26 * time.Hour)
	if tomorrow.YearDay() == now.YearDay()+1 && monday.AddDate(0, 0, 7).After(tomorrow) {
		if !strings.Contains(body, `<p class="meta only-sm">Next class after today: ВМ · tomorrow, `+tomorrow.Format("15:04")+`</p>`) {
			t.Error("phones should say when the next class after today is")
		}
	}
}

// Today: the Now card shows the class in progress with its teacher (from
// the subject page), how much of it is past and what comes after; phones
// get the rest of the day in "Later today".
func TestTodayPage(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeCIST{events: []cist.Event{
		{Start: now.Add(-10 * time.Minute), End: now.Add(85 * time.Minute), Subject: "ООПро", Type: "Лк", Room: "DL", Groups: "КІУКІ-25-3"},
		{Start: now.Add(95 * time.Minute), End: now.Add(190 * time.Minute), Subject: "ВМ", Type: "Пз", Room: "165", Groups: "КІУКІ-25-3"},
	}}
	stud := lectureWithTeacher(t, src)
	const g = "/g/KIUKI-25-3"

	_, body, _ := stud.get(g)
	for _, want := range []string{
		`class="now-card is-now"`, "Now · 85 min left",
		`<span class="meta-item">Pavlenko O. V.</span>`,
		`<progress class="bar bar-thin" value="10" max="100"></progress>`,
		`<span class="text-tertiary">No meeting link</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Now card missing %s", want)
		}
	}
	if src.events[1].Start.Day() != now.Day() {
		return // the next class is tomorrow
	}
	next := src.events[1].Start.Format("15:04")
	for _, want := range []string{
		`<div class="only-md"><span class="meta">Next: ВМ · ` + next + `<span class="meta-item mono">`,
		`<section class="card only-sm">`,
		`<h2>Later today</h2>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Today missing %s", want)
		}
	}
	// Later today holds only the class after the Now card's.
	later := body[strings.Index(body, `<h2>Later today</h2>`):]
	later = later[:strings.Index(later, "</section>")]
	if !strings.Contains(later, "ВМ") || strings.Contains(later, "ООПро") {
		t.Errorf("later today:\n%s", later)
	}
}

func TestScheduleWithoutCISTLink(t *testing.T) {
	e := newEnv(t) // KIUKI-25-3 without a CIST id
	lead := e.signUp(t, "lead", store.RoleLeader)
	_, body, _ := lead.get("/g/KIUKI-25-3/schedule")
	if !strings.Contains(body, "not linked to a CIST timetable") || strings.Contains(body, "Sync with CIST now") {
		t.Fatalf("schedule without CIST:\n%s", body)
	}
	// An invalid ?week falls back to the current week.
	if code, _, _ := lead.get("/g/KIUKI-25-3/schedule?week=yesterday"); code != http.StatusOK {
		t.Fatalf("bad week: %d", code)
	}
}

func TestAPISchedule(t *testing.T) {
	now := time.Now().UTC()
	src := &fakeCIST{events: []cist.Event{
		{Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), Subject: "КЕ", Type: "Лб", Room: "DL", Groups: "КІУКІ-25-3"},
	}}
	e := newScheduleEnv(t, src)
	lead := e.signUp(t, "lead", store.RoleLeader)
	stud := e.signUp(t, "stud", store.RoleStudent)
	api := "/api/v1/groups/KIUKI-25-3"
	tok := lead.csrf()

	if code, body := stud.api("POST", api+"/schedule/sync", stud.csrf(), nil); code != http.StatusForbidden {
		t.Fatalf("student sync: %d %s", code, body)
	}
	code, body := lead.api("POST", api+"/schedule/sync", tok, nil)
	if code != http.StatusOK || !strings.Contains(string(body), `"event_count":1`) {
		t.Fatalf("sync: %d %s", code, body)
	}
	if code, _ := lead.api("POST", api+"/schedule/sync", tok, nil); code != http.StatusTooManyRequests {
		t.Fatalf("second sync: %d", code)
	}

	code, body = stud.api("GET", api+"/schedule", "", nil)
	var sch struct {
		Events []struct {
			Title      string `json:"title"`
			LessonType string `json:"lesson_type"`
		} `json:"events"`
		Upcoming  []struct{ Title string } `json:"upcoming"`
		HasSource bool                     `json:"has_source"`
		Sync      *struct {
			EventCount int `json:"event_count"`
		} `json:"sync"`
	}
	if code != http.StatusOK || json.Unmarshal(body, &sch) != nil {
		t.Fatalf("schedule: %d %s", code, body)
	}
	// The event may fall on tomorrow, which is still within the default 7 days.
	if len(sch.Events) != 1 || sch.Events[0].Title != "КЕ" || sch.Events[0].LessonType != "lab" || len(sch.Upcoming) != 1 ||
		!sch.HasSource || sch.Sync == nil || sch.Sync.EventCount != 1 {
		t.Fatalf("schedule JSON: %s", body)
	}
	if code, _ := stud.api("GET", api+"/schedule?from=2026-01-01&to=2026-12-31", "", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("too long a range: %d", code)
	}
	if code, _ := stud.api("GET", api+"/schedule?from=soon", "", nil); code != http.StatusBadRequest {
		t.Fatalf("bad date: %d", code)
	}
}
