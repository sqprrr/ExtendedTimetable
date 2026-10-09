package web

import (
	"cmp"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// weekView is one week of the schedule page.
type weekView struct {
	Start time.Time
	// Days are all seven days, for the day strip.
	Days []scheduleDay
	// Prev, Next and This are ?week= values for the navigation links.
	Prev, Next, This string
	IsThis           bool
	// Label is the week's dates: "5–11 October".
	Label string
	// Count is the number of classes in the week.
	Count int
	// Day is the selected day (?day=, else today in this week, else the
	// first day with classes); phones show only that one.
	Day string
	// View is "week" (every day, the default) or "day" (only Day).
	View string
}

type scheduleDay struct {
	Date time.Time
	// Key is the ?day= value: "2026-10-07".
	Key      string
	Today    bool
	Selected bool
	Events   []*service.ScheduleEvent
}

// Optional reports whether the day's card is left out of the week list
// unless the day is selected: a weekend day without classes. Every day has
// a card, so the day strip can select any of them in place.
func (d scheduleDay) Optional() bool {
	wd := d.Date.Weekday()
	return (wd == time.Saturday || wd == time.Sunday) && len(d.Events) == 0
}

// classItem is what the scheduleEvent template renders.
type classItem struct {
	E *service.ScheduleEvent
	// Next marks the next class when none is in progress.
	Next bool
	// Past marks a class that has ended.
	Past bool
}

// weekStart returns Monday 00:00 of the week containing the ?week= date
// (YYYY-MM-DD), or of the current week when it is missing or invalid.
func weekStart(param string, now time.Time, loc *time.Location) time.Time {
	day := service.StartOfDay(now, loc)
	if d, err := time.ParseInLocation(time.DateOnly, param, loc); err == nil {
		day = d
	}
	offset := (int(day.Weekday()) + 6) % 7 // Monday = 0
	return day.AddDate(0, 0, -offset)
}

func (h *Handler) schedulePage(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	q := r.URL.Query()
	h.showSchedule(w, r, g, http.StatusOK, q.Get("week"), q.Get("day"), q.Get("view"), "")
}

func (h *Handler) showSchedule(w http.ResponseWriter, r *http.Request, g *service.GroupView, status int, week, day, view, errMsg string) {
	// The service's clock, so "today" agrees with its Now and Next marks.
	now, loc := h.svc.Now(), h.svc.Location(r.Context())
	start := weekStart(week, now, loc)
	sch, err := h.svc.Schedule(r.Context(), g.ID, start, start.AddDate(0, 0, 7))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	this := weekStart("", now, loc)
	wv := &weekView{
		Start:  start,
		Prev:   start.AddDate(0, 0, -7).Format(time.DateOnly),
		Next:   start.AddDate(0, 0, 7).Format(time.DateOnly),
		This:   this.Format(time.DateOnly),
		IsThis: start.Equal(this),
		Label:  h.weekLabel(i18n.FromContext(r.Context()), start),
		Count:  len(sch.Events),
		View:   "week",
	}
	if view == "day" {
		wv.View = "day"
	}
	today := now.In(loc).Format(time.DateOnly)
	firstBusy := ""
	for i := range 7 {
		d := scheduleDay{Date: start.AddDate(0, 0, i)}
		d.Key = d.Date.Format(time.DateOnly)
		d.Today = d.Key == today
		end := start.AddDate(0, 0, i+1)
		for _, e := range sch.Events {
			if !e.StartsAt.Before(d.Date) && e.StartsAt.Before(end) {
				d.Events = append(d.Events, e)
			}
		}
		if day == d.Key {
			wv.Day = d.Key
		}
		if firstBusy == "" && len(d.Events) > 0 {
			firstBusy = d.Key
		}
		wv.Days = append(wv.Days, d)
	}
	if wv.Day == "" && wv.IsThis {
		wv.Day = today
	}
	if wv.Day == "" {
		wv.Day = cmp.Or(firstBusy, wv.Days[0].Key)
	}
	for i := range wv.Days {
		wv.Days[i].Selected = wv.Days[i].Key == wv.Day
	}
	h.render(w, r, status, "schedule", pageData{Group: g, Section: "schedule", Schedule: sch, Week: wv, Error: errMsg})
}

// weekLabel is a week's dates: "5–11 October", or "28 September – 4 October"
// across two months.
func (h *Handler) weekLabel(l *i18n.Localizer, start time.Time) string {
	end := start.AddDate(0, 0, 6)
	if start.Month() == end.Month() {
		return l.T("schedule.week_range", "From", start.Day(), "To", end.Day(), "Month", l.T("month.long."+monthKey(end)))
	}
	return l.T("schedule.week_range_months", "From", start.Day(), "FromMonth", l.T("month.long."+monthKey(start)),
		"To", end.Day(), "ToMonth", l.T("month.long."+monthKey(end)))
}

// syncSchedule fetches the schedule from CIST now (leader and editors only). A failed
// fetch is recorded and shown by the page's sync status.
func (h *Handler) syncSchedule(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	week, day, view := r.PostFormValue("week"), r.PostFormValue("day"), r.PostFormValue("view")
	_, err := h.svc.SyncScheduleNow(r.Context(), g.ID)
	var serr *service.SyncError
	if err != nil && !errors.As(err, &serr) {
		msg, status, ok := userMessage(r, err)
		if !ok {
			h.renderError(w, r, err)
			return
		}
		h.showSchedule(w, r, g, status, week, day, view, msg)
		return
	}
	back := "/g/" + g.Code + "/schedule"
	q := url.Values{}
	for k, v := range map[string]string{"week": week, "day": day, "view": view} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) > 0 {
		back += "?" + q.Encode()
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
