package service

import (
	"fmt"
	"time"
)

// HalloweenSeason is when the pages wear the Halloween themes: from
// 20 October 00:00 to 2 November 23:59, Kyiv time. Next year, change the
// two dates.
var HalloweenSeason = Season{From: "2026-10-20", To: "2026-11-02", Zone: "Europe/Kyiv"}

// Season is a run of whole days in a time zone.
type Season struct {
	// From and To are the first and the last day, as 2006-01-02; both are
	// part of the season.
	From, To string
	// Zone is the time zone the days are counted in.
	Zone string
}

// span is a season as the moment it starts and the moment after it ends.
type span struct{ start, end time.Time }

func (s span) contains(t time.Time) bool { return !t.Before(s.start) && t.Before(s.end) }

// span returns the season from the first moment of From to the midnight
// after To.
func (s Season) span() (span, error) {
	loc, err := time.LoadLocation(s.Zone)
	if err != nil {
		return span{}, fmt.Errorf("season zone: %w", err)
	}
	start, err := time.ParseInLocation(time.DateOnly, s.From, loc)
	if err != nil {
		return span{}, fmt.Errorf("season start: %w", err)
	}
	last, err := time.ParseInLocation(time.DateOnly, s.To, loc)
	if err != nil {
		return span{}, fmt.Errorf("season end: %w", err)
	}
	end := last.AddDate(0, 0, 1)
	if !end.After(start) {
		return span{}, fmt.Errorf("season %s – %s ends before it starts", s.From, s.To)
	}
	return span{start, end}, nil
}

// IsHalloween reports whether it is the Halloween season now.
func (s *Service) IsHalloween() bool { return s.halloween.contains(s.now()) }
