package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

func TestLoadTimeZone(t *testing.T) {
	for _, name := range []string{"", "Local", "Mars/Olympus", "../../etc/passwd", "/etc/localtime"} {
		if service.LoadTimeZone(name) != nil || service.IsTimeZoneChoice(name) {
			t.Errorf("%q should not be a time zone", name)
		}
	}
	for _, name := range append([]string{"Europe/Kiev", "America/Argentina/Buenos_Aires"}, service.Zones...) {
		if loc := service.LoadTimeZone(name); loc == nil || loc.String() != name {
			t.Errorf("%q should load, got %v", name, loc)
		}
	}
	if !service.IsTimeZoneChoice(service.TimeZoneAuto) {
		t.Error("auto is a choice")
	}
}

// "Today" is the viewer's day: late on Wednesday evening in Kyiv, it is
// already Thursday in Tokyo.
func TestTodayIsTheViewersDay(t *testing.T) {
	f := setupSchedule(t)
	gid := f.group.ID
	f.cist.set([]cist.Event{f.at(2, 13, 10, "ООПро", "Пз"), f.at(3, 11, 15, "МОАП", "Лб")}, nil)
	if _, err := f.svc.SyncScheduleNow(f.lead, gid); err != nil {
		t.Fatal(err)
	}
	*f.now = time.Date(2026, 10, 7, 23, 30, 0, 0, f.kyiv)

	today := func(ctx context.Context) string {
		t.Helper()
		ov, err := f.svc.GroupOverview(ctx, gid)
		if err != nil {
			t.Fatal(err)
		}
		var out string
		for _, e := range ov.Today.Events {
			out += e.SubjectBrief
		}
		return out
	}
	if got := today(f.stud); got != "ООПро" {
		t.Errorf("site's zone: today = %q, want Wednesday's class", got)
	}
	tokyo := f.svc.WithTimeZone(f.stud, service.TimeZoneAuto, "Asia/Tokyo")
	if got := today(tokyo); got != "МОАП" {
		t.Errorf("device in Tokyo: today = %q, want Thursday's class", got)
	}
	if got := f.svc.Location(tokyo).String(); got != "Asia/Tokyo" || service.DeviceTimeZone(tokyo) != "Asia/Tokyo" {
		t.Errorf("location = %s, device = %s", got, service.DeviceTimeZone(tokyo))
	}
	// A zone the user chose wins over the device's; an unknown device zone
	// leaves the site's.
	if got := today(f.svc.WithTimeZone(f.stud, "Europe/Kyiv", "Asia/Tokyo")); got != "ООПро" {
		t.Errorf("chosen Kyiv: today = %q", got)
	}
	unknown := f.svc.WithTimeZone(f.stud, service.TimeZoneAuto, "Mars/Olympus")
	if f.svc.Location(unknown) != f.kyiv || service.DeviceTimeZone(unknown) != "" {
		t.Errorf("unknown device zone: %s %q", f.svc.Location(unknown), service.DeviceTimeZone(unknown))
	}
}

func TestTimeZoneChoiceIsKeptInTheAccount(t *testing.T) {
	f := setup(t)
	sess := f.register(t, "alice")
	ctx := f.as(t, sess)
	if got := service.TimeZoneChoice(ctx, "Asia/Tokyo"); got != "Asia/Tokyo" {
		t.Fatalf("no choice in the account: the cookie's, got %q", got)
	}
	wantInputError(t, f.svc.SetTimeZone(ctx, "Local"), "time_zone")
	if err := f.svc.SetTimeZone(ctx, "Europe/Warsaw"); err != nil {
		t.Fatal(err)
	}
	// The account's choice wins over the browser's cookie, also in a new
	// session, and login hands it to the browser.
	if got := service.TimeZoneChoice(f.as(t, sess), "Asia/Tokyo"); got != "Europe/Warsaw" {
		t.Errorf("choice = %q, want the account's", got)
	}
	login, err := f.svc.Login(context.Background(), service.LoginInput{Username: "alice", Password: "correct horse", ClientIP: "192.0.2.1"})
	if err != nil || login.TimeZone != "Europe/Warsaw" {
		t.Fatalf("login: %v %+v", err, login)
	}
	if got := service.TimeZoneChoice(context.Background(), "nonsense"); got != service.TimeZoneAuto {
		t.Errorf("visitor with a bad cookie: %q, want auto", got)
	}
}
