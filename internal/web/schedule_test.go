package web

import (
	"testing"
	"time"
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
