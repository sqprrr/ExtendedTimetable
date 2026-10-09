package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// setDeviceZone stores the cookie in which app.js reports the device's zone.
func (b *browser) setDeviceZone(tz string) {
	u, _ := url.Parse(b.e.srv.URL)
	b.c.Jar.SetCookies(u, []*http.Cookie{{Name: "__Host-extt_device_tz", Value: tz, Path: "/", Secure: true}})
}

// bodyTag returns the page's <body …> start tag.
func bodyTag(body string) string {
	i := strings.Index(body, "<body")
	if i < 0 {
		return ""
	}
	return body[i : i+strings.Index(body[i:], ">")+1]
}

// Classes run on Kyiv time; each user sees them in their own zone: the
// device's until they pick one.
func TestScheduleInTheViewersTimeZone(t *testing.T) {
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	ny, _ := time.LoadLocation("America/New_York")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	start := time.Now().Truncate(time.Minute).Add(3 * time.Hour)
	src := &fakeCIST{events: []cist.Event{
		{Start: start, End: start.Add(95 * time.Minute), Subject: "ВМ", Type: "Лк", Room: "287", Groups: "КІУКІ-25-3"},
	}}
	e := newScheduleEnvIn(t, src, kyiv)
	if _, err := e.svc.AdminSyncSchedule(context.Background(), "KIUKI-25-3"); err != nil {
		t.Fatal(err)
	}
	stud := e.signUp(t, "stud", store.RoleStudent)
	// The week of the class in loc, so the class is on the page whatever the day.
	schedule := func(loc *time.Location) string {
		t.Helper()
		code, body, _ := stud.get("/g/KIUKI-25-3/schedule?week=" + start.In(loc).Format(time.DateOnly))
		if code != http.StatusOK {
			t.Fatalf("schedule: %d", code)
		}
		return body
	}
	clock := func(loc *time.Location) string { return ">" + start.In(loc).Format("15:04") }

	// Nothing known: the site's zone, no note, and app.js is asked to
	// report the device's zone.
	body := schedule(kyiv)
	if !strings.Contains(body, clock(kyiv)) || strings.Contains(body, "zone-note") {
		t.Errorf("site's zone: want %s and no note", clock(kyiv))
	}
	_, kyivOffset := time.Now().In(kyiv).Zone()
	want := fmt.Sprintf(`data-tz-offset="%d" data-tz-cookie="__Host-extt_device_tz" data-tz-device="" data-tz-reload`, kyivOffset/60)
	if tag := bodyTag(body); !strings.Contains(tag, want) {
		t.Errorf("body: %s", tag)
	}
	if !strings.Contains(body, `<option value="auto" selected>Automatic</option>`) {
		t.Error("the picker should be on Automatic")
	}

	// The device is in New York.
	stud.setDeviceZone("America/New_York")
	body = schedule(ny)
	if !strings.Contains(body, clock(ny)) || strings.Contains(body, clock(kyiv)) {
		t.Errorf("device's zone: want %s, not %s", clock(ny), clock(kyiv))
	}
	for _, want := range []string{
		"Times are in New York time (UTC−", "Classes run on Kyiv time (UTC&#43;",
		`href="/g/KIUKI-25-3/more#time-zone"`,
		`<option value="auto" selected>Automatic: New York (UTC−`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("device's zone: missing %s", want)
		}
	}
	if tag := bodyTag(body); !strings.Contains(tag, `data-tz-offset="-`) || !strings.Contains(tag, `data-tz-device="America/New_York"`) {
		t.Errorf("body: %s", tag)
	}

	// Picking a zone wins over the device's and stops the reloads.
	code, _, h := stud.submit("/g/KIUKI-25-3/schedule", "/timezone", url.Values{"tz": {"Asia/Tokyo"}, "back": {"/g/KIUKI-25-3/schedule"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/g/KIUKI-25-3/schedule" {
		t.Fatalf("pick: %d %s", code, h.Get("Location"))
	}
	body = schedule(tokyo)
	if !strings.Contains(body, clock(tokyo)) || !strings.Contains(body, "Times are in Tokyo time (UTC&#43;9)") {
		t.Errorf("chosen zone: want %s", clock(tokyo))
	}
	if !strings.Contains(body, `<option value="Asia/Tokyo" selected>Tokyo (UTC&#43;9)</option>`) || strings.Contains(bodyTag(body), "data-tz-reload") {
		t.Error("the picker should show Tokyo, and the page should not follow the device")
	}

	// The choice follows the user to another browser.
	other := e.browser(t)
	other.submit("/login", "/login", url.Values{"username": {"stud"}, "password": {"correct horse"}})
	if _, body, _ := other.get("/g/KIUKI-25-3/schedule?week=" + start.In(tokyo).Format(time.DateOnly)); !strings.Contains(body, clock(tokyo)) {
		t.Error("the account's zone should apply in another browser")
	}

	// Automatic follows the device again; an unknown zone means automatic.
	stud.submit("/g/KIUKI-25-3/schedule", "/timezone", url.Values{"tz": {"auto"}, "back": {"/"}})
	if body := schedule(ny); !strings.Contains(body, clock(ny)) {
		t.Errorf("auto: want the device's %s", clock(ny))
	}
	stud.submit("/g/KIUKI-25-3/schedule", "/timezone", url.Values{"tz": {"Local"}, "back": {"/"}})
	if body := schedule(ny); !strings.Contains(body, clock(ny)) || !strings.Contains(body, `<option value="auto" selected>`) {
		t.Error("an unknown zone should fall back to automatic")
	}
	// A device zone the server does not know leaves the site's.
	stud.setDeviceZone("Mars/Olympus")
	if body := schedule(kyiv); !strings.Contains(body, clock(kyiv)) || !strings.Contains(bodyTag(body), `data-tz-device="Mars/Olympus"`) {
		t.Error("an unknown device zone should show the site's, and app.js should not report it again")
	}
}

// Deadlines are entered and shown in the viewer's zone.
func TestDeadlinesInTheViewersTimeZone(t *testing.T) {
	e := newEnv(t) // the site in UTC
	lead := e.signUp(t, "lead", store.RoleLeader)
	hwURL := setupHomework(t, e, lead)
	lead.submit("/", "/timezone", url.Values{"tz": {"Asia/Tokyo"}, "back": {"/"}})

	_, form, _ := lead.get(hwURL + "/edit")
	if !strings.Contains(form, "In Tokyo time (UTC&#43;9).") {
		t.Error("the deadline field should say which zone it is in")
	}
	m := regexp.MustCompile(`<option value="(\d+)"[^>]*selected[^>]*>Physics</option>`).FindStringSubmatch(form)
	if m == nil {
		t.Fatalf("no selected subject in the edit form")
	}
	if code, _, _ := lead.submit(hwURL+"/edit", hwURL, url.Values{
		"subject_id": {m[1]}, "title": {"Lab 1"}, "due_at": {"2026-12-01T10:00"},
	}); code != http.StatusSeeOther {
		t.Fatalf("edit: %d", code)
	}
	// 10:00 in Tokyo is 01:00 UTC.
	code, raw := lead.api("GET", "/api/v1/groups/KIUKI-25-3"+strings.TrimPrefix(hwURL, "/g/KIUKI-25-3"), "", nil)
	var hw struct {
		DueAt time.Time `json:"due_at"`
	}
	if code != http.StatusOK || json.Unmarshal(raw, &hw) != nil || !hw.DueAt.Equal(time.Date(2026, 12, 1, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("due_at: %d %s", code, raw)
	}
	if _, form, _ = lead.get(hwURL + "/edit"); !strings.Contains(form, `value="2026-12-01T10:00"`) {
		t.Error("the edit form should show the deadline in Tokyo time")
	}
	// A student on the site's zone sees it at 01:00.
	stud := e.signUp(t, "stud", store.RoleStudent)
	if _, page, _ := stud.get(hwURL); !strings.Contains(page, "01:00") || strings.Contains(page, "In Tokyo time") {
		t.Error("a student in UTC should see the deadline at 01:00")
	}
}

func TestTimeZoneChosenBeforeRegistering(t *testing.T) {
	e := newEnv(t)
	b := e.browser(t)
	b.submit("/login", "/timezone", url.Values{"tz": {"Europe/Warsaw"}, "back": {"/login"}})
	b.register("KIUKI-25-3", "alice")
	code, raw := b.api("GET", "/api/v1/me", "", nil)
	var me map[string]any
	if code != http.StatusOK || json.Unmarshal(raw, &me) != nil || me["time_zone"] != "Europe/Warsaw" || me["location"] != "Europe/Warsaw" {
		t.Fatalf("me: %d %s", code, raw)
	}
}

func TestAPITimeZone(t *testing.T) {
	e := newEnv(t)
	b := e.signUp(t, "api", store.RoleStudent)
	b.setDeviceZone("Europe/Berlin")
	tok := b.csrf()
	me := func(method string, body any) (int, map[string]any) {
		t.Helper()
		code, raw := b.api(method, "/api/v1/me", tok, body)
		var out map[string]any
		json.Unmarshal(raw, &out)
		return code, out
	}
	if code, out := me("GET", nil); code != http.StatusOK || out["time_zone"] != "auto" || out["location"] != "Europe/Berlin" {
		t.Fatalf("GET: %d %v", code, out)
	}
	if code, out := me("PUT", map[string]string{"time_zone": "America/Argentina/Buenos_Aires"}); code != http.StatusOK ||
		out["time_zone"] != "America/Argentina/Buenos_Aires" || out["location"] != "America/Argentina/Buenos_Aires" {
		t.Fatalf("PUT zone: %d %v", code, out)
	}
	// A zone outside the picker's list is offered as it is.
	if _, body, _ := b.get("/more"); !strings.Contains(body, `<option value="America/Argentina/Buenos_Aires" selected>Buenos Aires (UTC−3)</option>`) {
		t.Error("the picker should show the chosen zone")
	}
	if code, out := me("PUT", map[string]string{"time_zone": "auto"}); code != http.StatusOK || out["location"] != "Europe/Berlin" {
		t.Fatalf("PUT auto: %d %v", code, out)
	}
	if code, out := me("PUT", map[string]string{"time_zone": "Mars/Olympus"}); code != http.StatusUnprocessableEntity || out["code"] != "err.time_zone" {
		t.Fatalf("PUT bad zone: %d %v", code, out)
	}
}
