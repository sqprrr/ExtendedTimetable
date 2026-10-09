package web

import (
	"net/http"
	"slices"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Subject page tabs, chosen with ?tab=.
const (
	subjectTabHomework   = "homework"
	subjectTabRecordings = "recordings"
)

// subjectPage shows one subject: who teaches it, its class links and DL
// page, and a tab each for its homework and its recordings and solutions.
// The tab and the recordings' lesson type are in the URL
// (?tab=recordings&lesson=lab), so the page works without JavaScript.
func (h *Handler) subjectPage(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	p, err := h.svc.SubjectPage(r.Context(), g.ID, id)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	q := r.URL.Query()
	d := pageData{Group: g, Section: "subjects", SubjectPage: p, SubjectTab: subjectTabHomework, Query: q}
	if q.Get("tab") == subjectTabRecordings {
		d.SubjectTab = subjectTabRecordings
	}
	if lt := store.LessonType(q.Get("lesson")); slices.Contains(service.LessonTypes, lt) {
		d.Lesson = lt
	}
	d.Resources = p.Resources
	if d.Lesson != "" {
		d.Resources = nil
		for _, res := range p.Resources {
			if res.LessonType == d.Lesson {
				d.Resources = append(d.Resources, res)
			}
		}
	}
	d.HomeworkList = p.Homework
	d.HomeworkGroups = groupHomework(h.svc.Location(r.Context()), h.svc.Now(), p.Homework, service.HomeworkFilter{SubjectID: id})
	h.render(w, r, http.StatusOK, "subject", d)
}
