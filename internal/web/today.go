package web

import (
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// nowCard is the first card on Today (service.NowClass) with its overline.
type nowCard struct {
	*service.NowClass
	// State is "Now · 25 min left", "Next · in 15 min" or
	// "Next · tomorrow, 07:45".
	State string
}

// newNowCard words the Now card's overline; times are in loc. It returns
// nil when nothing is coming up.
func newNowCard(l *i18n.Localizer, loc *time.Location, now time.Time, c *service.NowClass) *nowCard {
	if c == nil {
		return nil
	}
	card := &nowCard{NowClass: c}
	switch {
	case c.Live:
		card.State = l.T("today.now_left", "Count", minutesUntil(now, now.Add(c.Left)))
	case c.Left < time.Hour:
		card.State = l.T("today.next_in", "Count", minutesUntil(now, now.Add(c.Left)))
	default:
		card.State = l.T("today.next_at", "When", relWhen(l, loc, now, c.Event.StartsAt))
	}
	return card
}
