package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// updateProgress changes the viewer's own status or grade for an assignment.
// The status toggle in homework lists sends view=item and gets the list item
// back; the panel on the homework page sends view=panel and gets the panel.
// Without htmx it redirects back to the page the form was on.
func (h *Handler) updateProgress(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	in, gradeInput, err := progressInput(r)
	var hw *service.Homework
	if err == nil {
		hw, err = h.svc.UpdateProgress(r.Context(), g.ID, id, in)
	}
	htmx := r.Header.Get("HX-Request") == "true"
	view := r.PostFormValue("view")
	if err != nil {
		msg, status, ok := userMessage(r, err)
		if !ok {
			h.renderError(w, r, err)
			return
		}
		if htmx && view != "panel" {
			// A list toggle has nowhere to show an error; htmx does not swap
			// it, and app.js shows this text instead.
			http.Error(w, msg, status)
			return
		}
		// Show the panel again with the error and what the user typed.
		if hw, err = h.svc.Homework(r.Context(), g.ID, id); err != nil {
			h.renderError(w, r, err)
			return
		}
		item := &hwItem{Code: g.Code, CSRF: auth.CSRFToken(r.Context()), HW: hw, From: "detail", Error: msg, GradeInput: gradeInput}
		if htmx {
			// htmx only swaps successful responses.
			h.renderFragment(w, r, "progressPanel", item)
			return
		}
		h.render(w, r, status, "homework_detail", pageData{Group: g, Section: "homework", Homework: hw, ProgressPanel: item})
		return
	}
	item := hwItem{Code: g.Code, CSRF: auth.CSRFToken(r.Context()), HW: hw, From: r.PostFormValue("from")}
	switch {
	case htmx && view == "panel":
		// The Overdue badge above the panel depends on the status too.
		item.UpdateBadge = true
		h.renderFragment(w, r, "progressPanel", item)
	case htmx:
		h.renderFragment(w, r, "homeworkItem", item)
	default:
		back := "/g/" + g.Code + "/homework/" + strconv.FormatInt(id, 10)
		switch item.From {
		case "overview":
			back = "/g/" + g.Code
		case "list":
			back = "/g/" + g.Code + "/homework"
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
	}
}

// progressInput reads the status and grade fields. A form without a grade
// field leaves the grade alone; an empty one clears it.
func progressInput(r *http.Request) (service.ProgressInput, string, error) {
	var in service.ProgressInput
	if s := r.PostFormValue("status"); s != "" {
		st := store.ProgressStatus(s)
		in.Status = &st
	}
	values, ok := r.PostForm["grade"]
	if !ok {
		return in, "", nil
	}
	in.SetGrade = true
	raw := strings.TrimSpace(values[0])
	if raw == "" {
		return in, raw, nil
	}
	g, err := strconv.ParseFloat(strings.Replace(raw, ",", ".", 1), 64)
	if err != nil {
		return in, raw, &service.InputError{Field: "grade", Msg: i18n.M("err.grade_nan")}
	}
	in.Grade = &g
	return in, raw, nil
}

func (h *Handler) myGrades(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	grades, err := h.svc.MyGrades(r.Context(), g.ID)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "grades", pageData{Group: g, Section: "grades", Grades: grades})
}
