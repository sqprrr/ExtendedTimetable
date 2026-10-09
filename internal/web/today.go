package web

import (
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// nowCard is the first card on Today: the class in progress, or the next
// one, with how long is left and its meeting links.
type nowCard struct {
	E *service.ScheduleEvent
	// Live is true while the class is in progress.
	Live bool
	// State is the card's overline: "Now · 25 min left", "Next · in 15 min",
	// "Next · tomorrow, 07:45".
	State string
	// Elapsed and Total (seconds) fill the progress bar of a live class.
	Elapsed, Total int64
	// After is the class after a live one, if there is one today.
	After *service.ScheduleEvent
}

// newNowCard picks the class for the Now card from today's schedule, or
// returns nil when nothing is coming up. Times are in loc.
func newNowCard(l *i18n.Localizer, loc *time.Location, now time.Time, sch *service.Schedule) *nowCard {
	if sch == nil || len(sch.Upcoming) == 0 {
		return nil
	}
	e := sch.Upcoming[0]
	c := &nowCard{E: e, Live: e.Now}
	switch {
	case c.Live:
		c.State = l.T("today.now_left", "Count", minutesUntil(now, e.EndsAt))
		c.Total = int64(e.EndsAt.Sub(e.StartsAt) / time.Second)
		c.Elapsed = min(c.Total, int64(now.Sub(e.StartsAt)/time.Second))
		for _, next := range sch.Events {
			if !next.StartsAt.Before(e.EndsAt) {
				c.After = next
				break
			}
		}
	case e.StartsAt.Sub(now) < time.Hour:
		c.State = l.T("today.next_in", "Count", minutesUntil(now, e.StartsAt))
	default:
		c.State = l.T("today.next_at", "When", relWhen(l, loc, now, e.StartsAt))
	}
	return c
}
