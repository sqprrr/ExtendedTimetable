package api

import (
	"net/http"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

type classJSON struct {
	ID       int64     `json:"id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	// SubjectID is null when the class is not linked to a subject; Title is
	// then CIST's short name.
	SubjectID    *int64           `json:"subject_id"`
	Title        string           `json:"title"`
	SubjectBrief string           `json:"subject_brief"`
	CISTType     string           `json:"cist_type"`
	LessonType   store.LessonType `json:"lesson_type,omitempty"`
	Room         string           `json:"room"`
	Groups       string           `json:"groups"`
	Now          bool             `json:"now"`
	ClassLinks   []classLinkJSON  `json:"class_links"`
}

type syncJSON struct {
	LastAttemptAt time.Time  `json:"last_attempt_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     string     `json:"last_error"`
	EventCount    int        `json:"event_count"`
}

type scheduleJSON struct {
	Events   []classJSON `json:"events"`
	Upcoming []classJSON `json:"upcoming"`
	// HasSource is false when the group is not linked to CIST.
	HasSource bool      `json:"has_source"`
	Sync      *syncJSON `json:"sync"`
}

func toClass(e *service.ScheduleEvent) classJSON {
	return classJSON{
		ID: e.ID, StartsAt: e.StartsAt, EndsAt: e.EndsAt, SubjectID: e.SubjectID, Title: e.Title(),
		SubjectBrief: e.SubjectBrief, CISTType: e.CISTType, LessonType: e.LessonType, Room: e.Room, Groups: e.Groups,
		Now: e.Now, ClassLinks: list(e.ClassLinks, toClassLink),
	}
}

func toSync(s *store.ScheduleSync) *syncJSON {
	if s == nil {
		return nil
	}
	return &syncJSON{LastAttemptAt: s.LastAttemptAt, LastSuccessAt: s.LastSuccessAt, LastError: s.LastError, EventCount: s.EventCount}
}

// schedule returns the classes that start between ?from and ?to (dates,
// YYYY-MM-DD, to exclusive), by default the seven days from today.
func (h *Handler) schedule(w http.ResponseWriter, r *http.Request) {
	gid := h.group(w, r)
	if gid == 0 {
		return
	}
	loc := h.svc.Location(r.Context())
	from := service.StartOfDay(h.svc.Now(), loc)
	if s := r.URL.Query().Get("from"); s != "" {
		d, err := time.ParseInLocation(time.DateOnly, s, loc)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "from must be a date like 2026-10-05")
			return
		}
		from = d
	}
	to := from.AddDate(0, 0, 7)
	if s := r.URL.Query().Get("to"); s != "" {
		d, err := time.ParseInLocation(time.DateOnly, s, loc)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "to must be a date like 2026-10-12")
			return
		}
		to = d
	}
	sch, err := h.svc.Schedule(r.Context(), gid, from, to)
	reply(h, w, r, http.StatusOK, sch, err, func(s *service.Schedule) scheduleJSON {
		return scheduleJSON{
			Events: list(s.Events, toClass), Upcoming: list(s.Upcoming, toClass),
			HasSource: s.HasSource, Sync: toSync(s.Sync),
		}
	})
}

// syncSchedule fetches the schedule from CIST now (leader and editors only).
func (h *Handler) syncSchedule(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		rec, err := h.svc.SyncScheduleNow(r.Context(), gid)
		reply(h, w, r, http.StatusOK, rec, err, toSync)
	}
}
