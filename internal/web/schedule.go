package web

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// weekView is one week of the schedule page.
type weekView struct {
	Start time.Time
	Days  []scheduleDay
	// Prev, Next and This are ?week= values for the navigation links.
	Prev, Next, This string
	IsThis           bool
}

type scheduleDay struct {
	Date   time.Time
	Today  bool
	Events []*service.ScheduleEvent
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
	h.showSchedule(w, r, g, http.StatusOK, r.URL.Query().Get("week"), "")
}

func (h *Handler) showSchedule(w http.ResponseWriter, r *http.Request, g *service.GroupView, status int, week, errMsg string) {
	// The service's clock, so "today" agrees with its Now and Next marks.
	now := h.svc.Now()
	start := weekStart(week, now, h.loc)
	sch, err := h.svc.Schedule(r.Context(), g.ID, start, start.AddDate(0, 0, 7))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	this := weekStart("", now, h.loc)
	wv := &weekView{
		Start:  start,
		Prev:   start.AddDate(0, 0, -7).Format(time.DateOnly),
		Next:   start.AddDate(0, 0, 7).Format(time.DateOnly),
		This:   this.Format(time.DateOnly),
		IsThis: start.Equal(this),
	}
	today := now.In(h.loc).Format(time.DateOnly)
	for i := range 7 {
		day := scheduleDay{Date: start.AddDate(0, 0, i)}
		day.Today = day.Date.Format(time.DateOnly) == today
		end := start.AddDate(0, 0, i+1)
		for _, e := range sch.Events {
			if !e.StartsAt.Before(day.Date) && e.StartsAt.Before(end) {
				day.Events = append(day.Events, e)
			}
		}
		// The weekend shows only when it has classes.
		if i < 5 || len(day.Events) > 0 {
			wv.Days = append(wv.Days, day)
		}
	}
	h.render(w, r, status, "schedule", pageData{Group: g, Section: "schedule", Schedule: sch, Week: wv, Error: errMsg})
}

// syncSchedule fetches the schedule from CIST now (leaders only). A failed
// fetch is recorded and shown by the page's sync status.
func (h *Handler) syncSchedule(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	week := r.PostFormValue("week")
	_, err := h.svc.SyncScheduleNow(r.Context(), g.ID)
	var serr *service.SyncError
	if err != nil && !errors.As(err, &serr) {
		msg, status, ok := userMessage(r, err)
		if !ok {
			h.renderError(w, r, err)
			return
		}
		h.showSchedule(w, r, g, status, week, msg)
		return
	}
	back := "/g/" + g.Code + "/schedule"
	if week != "" {
		back += "?week=" + url.QueryEscape(week)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
