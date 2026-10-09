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

func TestNowCard(t *testing.T) {
	en := i18n.For("en")
	now := time.Date(2026, 10, 7, 10, 40, 0, 0, time.UTC)
	ev := func(id int64, start, end string, live bool) *service.ScheduleEvent {
		s, _ := time.Parse(time.DateTime, "2026-10-07 "+start+":00")
		e, _ := time.Parse(time.DateTime, "2026-10-07 "+end+":00")
		return &service.ScheduleEvent{ScheduleEvent: store.ScheduleEvent{ID: id, StartsAt: s, EndsAt: e, SubjectBrief: "ООП"}, Now: live}
	}
	oop, math := ev(1, "09:30", "11:05", true), ev(2, "11:15", "12:50", false)

	c := newNowCard(en, time.UTC, now, &service.Schedule{Events: []*service.ScheduleEvent{oop, math}, Upcoming: []*service.ScheduleEvent{oop}})
	if c == nil || !c.Live || c.State != "Now · 25 min left" || c.After != math || c.Elapsed != 70*60 || c.Total != 95*60 {
		t.Fatalf("live card: %+v", c)
	}
	c = newNowCard(en, time.UTC, now.Add(30*time.Minute), &service.Schedule{Events: []*service.ScheduleEvent{math}, Upcoming: []*service.ScheduleEvent{math}})
	if c == nil || c.Live || c.State != "Next · in 5 min" {
		t.Fatalf("next soon: %+v", c)
	}
	early := ev(1, "09:30", "11:05", false)
	c = newNowCard(en, time.UTC, now.Add(-3*time.Hour), &service.Schedule{Upcoming: []*service.ScheduleEvent{early}})
	if c == nil || c.State != "Next · today, 09:30" {
		t.Fatalf("next later: %+v", c)
	}
	if c := newNowCard(en, time.UTC, now, &service.Schedule{}); c != nil {
		t.Fatalf("nothing upcoming: %+v", c)
	}
}
