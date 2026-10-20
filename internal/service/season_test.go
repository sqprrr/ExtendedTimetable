package service_test

import (
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// The Halloween season runs from 20 October 00:00 to 2 November 23:59 in
// Kyiv, whatever the site's own time zone.
func TestHalloweenSeason(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}
	s := service.New(nil, service.Config{Location: time.UTC})
	for _, c := range []struct {
		at   time.Time
		want bool
	}{
		{time.Date(2026, 10, 19, 23, 59, 59, 0, kyiv), false},
		{time.Date(2026, 10, 20, 0, 0, 0, 0, kyiv), true},
		{time.Date(2026, 10, 19, 21, 0, 0, 0, time.UTC), true}, // 20 Oct 00:00 in Kyiv (UTC+3)
		{time.Date(2026, 10, 31, 20, 0, 0, 0, kyiv), true},
		{time.Date(2026, 11, 2, 23, 59, 59, 0, kyiv), true},
		{time.Date(2026, 11, 3, 0, 0, 0, 0, kyiv), false},
		{time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC), false}, // 3 Nov 00:00 in Kyiv (UTC+2)
		{time.Date(2027, 10, 25, 12, 0, 0, 0, kyiv), false},    // next year needs new dates
	} {
		service.SetClock(s, func() time.Time { return c.at })
		if got := s.IsHalloween(); got != c.want {
			t.Errorf("IsHalloween at %s = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestSeasonFromConfig(t *testing.T) {
	s := service.New(nil, service.Config{Halloween: service.Season{From: "2030-01-01", To: "2030-01-01", Zone: "UTC"}})
	service.SetClock(s, func() time.Time { return time.Date(2030, 1, 1, 23, 59, 0, 0, time.UTC) })
	if !s.IsHalloween() {
		t.Error("a one-day season should hold the whole day")
	}
	service.SetClock(s, func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) })
	if s.IsHalloween() {
		t.Error("the configured season should replace HalloweenSeason")
	}
}

func TestBadSeasonPanics(t *testing.T) {
	for _, season := range []service.Season{
		{From: "2026-11-02", To: "2026-10-20", Zone: "Europe/Kyiv"},
		{From: "20.10.2026", To: "2026-11-02", Zone: "Europe/Kyiv"},
		{From: "2026-10-20", To: "2026-11-31", Zone: "Europe/Kyiv"},
		{From: "2026-10-20", To: "2026-11-02", Zone: "Mars/Olympus"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%v: want a panic", season)
				}
			}()
			service.New(nil, service.Config{Halloween: season})
		}()
	}
}
