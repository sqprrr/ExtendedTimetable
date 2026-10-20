package service

import "time"

// NowClass is the first card on Today: the class in progress, or the next
// one.
type NowClass struct {
	Event *ScheduleEvent
	// Live is true while the class is in progress.
	Live bool
	// Left is the time until a live class ends, or until the next one
	// starts.
	Left time.Duration
	// Elapsed is the share of a live class already past, in percent.
	Elapsed int
	// After is the class that follows a live one today, if any.
	After *ScheduleEvent
}

// nowClass picks the class for Today's first card from today's schedule
// at now, or returns nil when nothing is coming up.
func nowClass(today *Schedule, now time.Time) *NowClass {
	if today == nil || len(today.Upcoming) == 0 {
		return nil
	}
	e := today.Upcoming[0]
	c := &NowClass{Event: e, Live: e.Now}
	if !c.Live {
		c.Left = e.StartsAt.Sub(now)
		return c
	}
	c.Left = e.EndsAt.Sub(now)
	if total := e.EndsAt.Sub(e.StartsAt); total > 0 {
		c.Elapsed = int(min(100, 100*now.Sub(e.StartsAt)/total))
	}
	for _, next := range today.Events {
		if !next.StartsAt.Before(e.EndsAt) {
			c.After = next
			break
		}
	}
	return c
}

// laterToday returns today's classes that start after now, but the one on
// the Now card.
func laterToday(today *Schedule, card *NowClass, now time.Time) []*ScheduleEvent {
	if today == nil {
		return nil
	}
	var out []*ScheduleEvent
	for _, e := range today.Events {
		if e.StartsAt.After(now) && (card == nil || e.ID != card.Event.ID) {
			out = append(out, e)
		}
	}
	return out
}
