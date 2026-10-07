package web

import (
	"math"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// Dates and times are relative when close and exact when far: "today,
// 09:30", "tomorrow, 09:30", "Fri, 23:59", "21 Oct", "2 days overdue".

// daysFrom is the number of calendar days from today to t's day in loc:
// 0 today, 1 tomorrow, -1 yesterday.
func daysFrom(now, t time.Time, loc *time.Location) int {
	a, b := service.StartOfDay(now, loc), service.StartOfDay(t, loc)
	// Whole days, rounded: a day across a clock change is 23 or 25 hours.
	return int(math.Round(b.Sub(a).Hours() / 24))
}

// shortDate is "21 Oct", with the year when it is not this year.
func (h *Handler) shortDate(l *i18n.Localizer, now, t time.Time) string {
	t = t.In(h.loc)
	month := l.T("month.short." + monthKey(t))
	if t.Year() != now.In(h.loc).Year() {
		return l.T("when.date_year", "Day", t.Day(), "Month", month, "Year", t.Year())
	}
	return l.T("when.date", "Day", t.Day(), "Month", month)
}

// longDate is a page subtitle's date: "Wednesday, 7 October".
func (h *Handler) longDate(l *i18n.Localizer, t time.Time) string {
	t = t.In(h.loc)
	return l.T("when.long_date", "Weekday", l.T("weekday.long."+weekdayKey(t)), "Day", t.Day(), "Month", l.T("month.long."+monthKey(t)))
}

// relWhen is a moment relative to now: "today, 09:30", "tomorrow, 09:30",
// "yesterday, 09:30", "Fri, 23:59" within the coming week, else "21 Oct,
// 09:30".
func (h *Handler) relWhen(l *i18n.Localizer, now, t time.Time) string {
	clock := t.In(h.loc).Format("15:04")
	switch d := daysFrom(now, t, h.loc); {
	case d == 0:
		return l.T("when.today", "Time", clock)
	case d == 1:
		return l.T("when.tomorrow", "Time", clock)
	case d == -1:
		return l.T("when.yesterday", "Time", clock)
	case d > 1 && d < 7:
		return l.T("when.weekday", "Day", l.T("weekday.short."+weekdayKey(t.In(h.loc))), "Time", clock)
	default:
		return l.T("when.date_time", "Date", h.shortDate(l, now, t), "Time", clock)
	}
}

// due says when an assignment is due: "due tomorrow, 09:30", "due 21 Oct",
// "2 days overdue", "no deadline".
func (h *Handler) due(l *i18n.Localizer, now time.Time, hw *service.Homework) string {
	if hw.DueAt == nil {
		return l.T("hw.no_deadline")
	}
	d := daysFrom(now, *hw.DueAt, h.loc)
	if hw.Overdue && d < -1 {
		return l.T("hw.overdue_days", "Count", -d)
	}
	if d >= 7 || d < -1 {
		return l.T("hw.due", "When", h.shortDate(l, now, *hw.DueAt))
	}
	return l.T("hw.due", "When", h.relWhen(l, now, *hw.DueAt))
}

// monthKey is the locale key of t's month: "jan", "feb", …
func monthKey(t time.Time) string {
	return [...]string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}[t.Month()-1]
}

// minutesUntil is how many whole minutes are left from now until t, rounded
// up so a class ending in 30 seconds still has "1 min left".
func minutesUntil(now, t time.Time) int {
	d := t.Sub(now)
	if d <= 0 {
		return 0
	}
	return int((d + time.Minute - 1) / time.Minute)
}
