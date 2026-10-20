package web

import (
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestWeekStart(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}
	// Sunday 25 October 2026, 23:30 in Kyiv: clocks went back that night.
	now := time.Date(2026, 10, 25, 23, 30, 0, 0, kyiv)
	for param, want := range map[string]string{
		"":           "2026-10-19",
		"2026-10-26": "2026-10-26", // a Monday stays
		"2026-11-01": "2026-10-26", // a Sunday goes back to its Monday
		"2026-10-07": "2026-10-05",
		"garbage":    "2026-10-19",
	} {
		got := weekStart(param, now, kyiv)
		if got.Format(time.DateOnly) != want || got.Hour() != 0 || got.Location() != kyiv {
			t.Errorf("weekStart(%q) = %v, want %s 00:00 Kyiv", param, got, want)
		}
	}
	// The week across the clock change is still seven calendar days.
	start := weekStart("2026-10-21", now, kyiv)
	if end := start.AddDate(0, 0, 7); end.Format("2006-01-02 15:04") != "2026-10-26 00:00" {
		t.Errorf("week end = %v", end)
	}
}

func TestWeekLabel(t *testing.T) {
	en, uk := i18n.For("en"), i18n.For("uk")
	oct := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	across := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		l      *i18n.Localizer
		start  time.Time
		months string
		want   string
	}{
		{en, oct, "month.long.", "5–11 October"},
		{en, oct, "month.short.", "5–11 Oct"},
		{en, across, "month.short.", "28 Sep – 4 Oct"},
		{uk, oct, "month.long.", "5–11 жовтня"},
	} {
		if got := weekLabel(c.l, c.start, c.months); got != c.want {
			t.Errorf("weekLabel(%s, %s) = %q, want %q", c.start.Format(time.DateOnly), c.months, got, c.want)
		}
	}
}

func TestNextAfterToday(t *testing.T) {
	at := func(day, hour int) *service.ScheduleEvent {
		return &service.ScheduleEvent{ScheduleEvent: store.ScheduleEvent{StartsAt: time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC)}}
	}
	tomorrow, weekEnd := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	today, thursday, monday := at(7, 9), at(8, 7), at(12, 7)

	if got := nextAfterToday(&service.Schedule{Events: []*service.ScheduleEvent{today, thursday}, Upcoming: []*service.ScheduleEvent{today}}, tomorrow, weekEnd); got != thursday {
		t.Errorf("a class later this week: %v", got)
	}
	// Nothing more this week: the next upcoming class, next week.
	if got := nextAfterToday(&service.Schedule{Events: []*service.ScheduleEvent{today}, Upcoming: []*service.ScheduleEvent{monday}}, tomorrow, weekEnd); got != monday {
		t.Errorf("next week: %v", got)
	}
	// Still today's class to come and nothing known after it.
	if got := nextAfterToday(&service.Schedule{Events: []*service.ScheduleEvent{today}, Upcoming: []*service.ScheduleEvent{today}}, tomorrow, weekEnd); got != nil {
		t.Errorf("nothing after today: %v", got)
	}
}
