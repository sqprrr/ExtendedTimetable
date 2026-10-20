package web

import (
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestRelativeDates(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	en, uk := i18n.For("en"), i18n.For("uk")
	// Wednesday 7 October 2026, 10:00 in Kyiv.
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, kyiv)
	at := func(day, hour, min int) *time.Time {
		t := time.Date(2026, 10, day, hour, min, 0, 0, kyiv)
		return &t
	}
	for _, c := range []struct {
		due     *time.Time
		overdue bool
		want    string
	}{
		{nil, false, "no deadline"},
		{at(7, 23, 59), false, "due today, 23:59"},
		{at(8, 9, 30), false, "due tomorrow, 09:30"},
		{at(9, 23, 59), false, "due Fri, 23:59"},
		{at(13, 9, 0), false, "due Tue, 09:00"},
		{at(14, 9, 0), false, "due 14 Oct"},
		{at(6, 18, 0), true, "due yesterday, 18:00"},
		{at(5, 18, 0), true, "2 days overdue"},
		{at(5, 18, 0), false, "due 5 Oct"}, // done: no longer overdue
		{at(7, 9, 0), true, "due today, 09:00"},
	} {
		hw := &service.Homework{Homework: store.Homework{DueAt: c.due}, Overdue: c.overdue}
		if got := due(en, kyiv, now, hw); got != c.want {
			t.Errorf("due %v (overdue %v) = %q, want %q", c.due, c.overdue, got, c.want)
		}
	}
	next := time.Date(2027, 1, 20, 9, 0, 0, 0, kyiv)
	if got := shortDate(en, kyiv, now, next); got != "20 Jan 2027" {
		t.Errorf("another year: %q", got)
	}
	if got := longDate(uk, kyiv, now); got != "Середа, 7 жовтня" {
		t.Errorf("uk long date: %q", got)
	}
	if got := due(uk, kyiv, now, &service.Homework{Homework: store.Homework{DueAt: at(3, 9, 0)}, Overdue: true}); got != "прострочено на 4 дні" {
		t.Errorf("uk overdue: %q", got)
	}
	// Across the switch to winter time (25 October) days stay whole.
	if got := relWhen(en, kyiv, time.Date(2026, 10, 24, 23, 0, 0, 0, kyiv), time.Date(2026, 10, 26, 1, 0, 0, 0, kyiv)); got != "Mon, 01:00" {
		t.Errorf("across DST: %q", got)
	}
}

// The Now card's overline; the service picks the class (service.NowClass).
func TestNowCard(t *testing.T) {
	en := i18n.For("en")
	now := time.Date(2026, 10, 7, 10, 40, 0, 0, time.UTC)
	ev := &service.ScheduleEvent{ScheduleEvent: store.ScheduleEvent{StartsAt: time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)}}
	for _, c := range []struct {
		class *service.NowClass
		want  string
	}{
		{&service.NowClass{Event: ev, Live: true, Left: 25 * time.Minute}, "Now · 25 min left"},
		{&service.NowClass{Event: ev, Live: true, Left: 30 * time.Second}, "Now · 1 min left"},
		{&service.NowClass{Event: ev, Left: 5 * time.Minute}, "Next · in 5 min"},
		{&service.NowClass{Event: ev, Left: 2 * time.Hour}, "Next · today, 09:30"},
	} {
		if got := newNowCard(en, time.UTC, now, c.class); got == nil || got.State != c.want || got.NowClass != c.class {
			t.Errorf("%+v: %+v, want %q", c.class, got, c.want)
		}
	}
	if c := newNowCard(en, time.UTC, now, nil); c != nil {
		t.Fatalf("nothing upcoming: %+v", c)
	}
}
