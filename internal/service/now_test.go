package service_test

import (
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestNowClass(t *testing.T) {
	now := time.Date(2026, 10, 7, 10, 40, 0, 0, time.UTC)
	ev := func(id int64, start, end string, live bool) *service.ScheduleEvent {
		s, _ := time.Parse(time.DateTime, "2026-10-07 "+start+":00")
		e, _ := time.Parse(time.DateTime, "2026-10-07 "+end+":00")
		return &service.ScheduleEvent{ScheduleEvent: store.ScheduleEvent{ID: id, StartsAt: s, EndsAt: e}, Now: live}
	}
	physics, oop, math, english := ev(1, "07:45", "09:20", false), ev(2, "09:30", "11:05", true), ev(3, "11:15", "12:50", false), ev(4, "13:10", "14:45", false)
	today := &service.Schedule{Events: []*service.ScheduleEvent{physics, oop, math, english}, Upcoming: []*service.ScheduleEvent{oop}}

	// A class in progress: 70 of its 95 minutes are past, 25 are left, and
	// the class after it comes next.
	c := service.PickNowClass(today, now)
	if c == nil || c.Event != oop || !c.Live || c.Left != 25*time.Minute || c.Elapsed != 73 || c.After != math {
		t.Fatalf("live: %+v", c)
	}
	if later := service.LaterToday(today, c, now); len(later) != 2 || later[0] != math || later[1] != english {
		t.Errorf("later today: %v", later)
	}

	// Between classes the card shows the next one, which Later leaves out.
	at := now.Add(30 * time.Minute)
	math.Now, oop.Now = false, false
	today.Upcoming = []*service.ScheduleEvent{math}
	c = service.PickNowClass(today, at)
	if c == nil || c.Event != math || c.Live || c.Left != 5*time.Minute || c.Elapsed != 0 || c.After != nil {
		t.Fatalf("next: %+v", c)
	}
	if later := service.LaterToday(today, c, at); len(later) != 1 || later[0] != english {
		t.Errorf("later today: %v", later)
	}

	// The last class of the day has nothing after it.
	english.Now = true
	today.Upcoming = []*service.ScheduleEvent{english}
	if c := service.PickNowClass(today, english.StartsAt.Add(time.Minute)); c == nil || c.After != nil || c.Elapsed != 1 {
		t.Fatalf("last class: %+v", c)
	}

	if c := service.PickNowClass(&service.Schedule{}, now); c != nil {
		t.Errorf("nothing upcoming: %+v", c)
	}
	if later := service.LaterToday(nil, nil, now); later != nil {
		t.Errorf("no schedule: %v", later)
	}
}

func TestTeacher(t *testing.T) {
	e := &service.ScheduleEvent{ScheduleEvent: store.ScheduleEvent{SubjectLecturer: "Pavlenko O. V.", SubjectInstructor: "Koval I. M."}}
	for typ, want := range map[store.LessonType]string{
		store.LessonLecture: "Pavlenko O. V.", store.LessonPractice: "Koval I. M.", store.LessonLab: "Koval I. M.", "": "",
	} {
		e.LessonType = typ
		if got := e.Teacher(); got != want {
			t.Errorf("%q: %q, want %q", typ, got, want)
		}
	}
}
