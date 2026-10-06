package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

const (
	dateTimeLayout = "2006-01-02T15:04"
	dateLayout     = "2006-01-02"
)

// kyiv is the timezone for due dates typed into the forms.
var kyiv = loadKyiv()

func loadKyiv() *time.Location {
	loc, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		return time.UTC
	}
	return loc
}

func fmtDateTime(t time.Time) string { return t.In(kyiv).Format("02.01.2006 15:04") }

func dateTimeInput(t time.Time) string { return t.In(kyiv).Format(dateTimeLayout) }

func (h *Handler) registerGroupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /groups/{code}", h.groupPage)

	mux.HandleFunc("POST /groups/{code}/subjects", h.act("subjects", h.saveSubject))
	mux.HandleFunc("POST /groups/{code}/subjects/{id}", h.act("subjects", h.saveSubject))
	mux.HandleFunc("POST /groups/{code}/subjects/{id}/delete", h.act("subjects", h.deleteSubject))

	mux.HandleFunc("POST /groups/{code}/class-links", h.act("class-links", h.saveClassLink))
	mux.HandleFunc("POST /groups/{code}/class-links/{id}", h.act("class-links", h.saveClassLink))
	mux.HandleFunc("POST /groups/{code}/class-links/{id}/delete", h.act("class-links", h.deleteClassLink))

	mux.HandleFunc("POST /groups/{code}/homework", h.act("homework", h.saveHomework))
	mux.HandleFunc("POST /groups/{code}/homework/{id}", h.act("homework", h.saveHomework))
	mux.HandleFunc("POST /groups/{code}/homework/{id}/delete", h.act("homework", h.deleteHomework))
	mux.HandleFunc("POST /groups/{code}/homework/{id}/links", h.act("homework", h.addHomeworkLink))
	mux.HandleFunc("POST /groups/{code}/homework-links/{id}/delete", h.act("homework", h.deleteHomeworkLink))

	mux.HandleFunc("POST /groups/{code}/notes", h.act("notes", h.saveNote))
	mux.HandleFunc("POST /groups/{code}/notes/{id}", h.act("notes", h.saveNote))
	mux.HandleFunc("POST /groups/{code}/notes/{id}/delete", h.act("notes", h.deleteNote))

	mux.HandleFunc("POST /groups/{code}/resources", h.act("resources", h.saveResource))
	mux.HandleFunc("POST /groups/{code}/resources/{id}", h.act("resources", h.saveResource))
	mux.HandleFunc("POST /groups/{code}/resources/{id}/delete", h.act("resources", h.deleteResource))
}

func (h *Handler) groupPage(w http.ResponseWriter, r *http.Request) {
	h.renderGroup(w, r, http.StatusOK, r.PathValue("code"), "")
}

func (h *Handler) renderGroup(w http.ResponseWriter, r *http.Request, status int, code, errMsg string) {
	page, err := h.svc.GroupPage(r.Context(), code)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, status, "group", pageData{Page: page, Error: errMsg})
}

// groupAction is a write on a group's content. id is the path's {id}, zero when absent.
type groupAction func(ctx context.Context, code string, id int64, r *http.Request) error

// act runs a write, then redirects back to the section, or re-renders the
// group page with the error.
func (h *Handler) act(section string, fn groupAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		id, err := pathID(r)
		if err != nil {
			h.renderError(w, r, service.ErrNotFound)
			return
		}
		if err := fn(r.Context(), code, id, r); err != nil {
			msg, status, ok := userMessage(err)
			if !ok {
				h.renderError(w, r, err)
				return
			}
			h.renderGroup(w, r, status, code, msg)
			return
		}
		http.Redirect(w, r, "/groups/"+url.PathEscape(code)+"#"+section, http.StatusSeeOther)
	}
}

func pathID(r *http.Request) (int64, error) {
	s := r.PathValue("id")
	if s == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, service.ErrNotFound
	}
	return id, nil
}

// formID reads a required select value that must be a positive id.
func formID(r *http.Request, field string) (int64, error) {
	id, err := strconv.ParseInt(r.PostFormValue(field), 10, 64)
	if err != nil || id <= 0 {
		return 0, &service.InputError{Field: field, Msg: "choose a value from the list"}
	}
	return id, nil
}

func parseDateTime(s string) (time.Time, error) {
	t, err := time.ParseInLocation(dateTimeLayout, strings.TrimSpace(s), kyiv)
	if err != nil {
		return time.Time{}, &service.InputError{Field: "due", Msg: "enter a valid date and time"}
	}
	return t, nil
}

// parseOptionalDate reads a YYYY-MM-DD field; an empty value is the zero time.
func parseOptionalDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.ParseInLocation(dateLayout, s, kyiv)
	if err != nil {
		return time.Time{}, &service.InputError{Field: "date", Msg: "enter a valid date"}
	}
	return t, nil
}

func parseOptionalFloat(s string) (*float64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, &service.InputError{Field: "max_points", Msg: "enter a number"}
	}
	return &f, nil
}

func (h *Handler) saveSubject(ctx context.Context, code string, id int64, r *http.Request) error {
	return h.svc.SaveSubject(ctx, code, id, service.SubjectInput{
		Name:      r.PostFormValue("name"),
		ShortName: r.PostFormValue("short_name"),
	})
}

func (h *Handler) deleteSubject(ctx context.Context, code string, id int64, _ *http.Request) error {
	return h.svc.DeleteSubject(ctx, code, id)
}

func (h *Handler) saveClassLink(ctx context.Context, code string, id int64, r *http.Request) error {
	subjectID, err := formID(r, "subject_id")
	if err != nil {
		return err
	}
	return h.svc.SaveClassLink(ctx, code, id, service.ClassLinkInput{
		SubjectID:  subjectID,
		LessonType: r.PostFormValue("lesson_type"),
		URL:        r.PostFormValue("url"),
		Note:       r.PostFormValue("note"),
	})
}

func (h *Handler) deleteClassLink(ctx context.Context, code string, id int64, _ *http.Request) error {
	return h.svc.DeleteClassLink(ctx, code, id)
}

func (h *Handler) saveHomework(ctx context.Context, code string, id int64, r *http.Request) error {
	subjectID, err := formID(r, "subject_id")
	if err != nil {
		return err
	}
	due, err := parseDateTime(r.PostFormValue("due"))
	if err != nil {
		return err
	}
	maxPoints, err := parseOptionalFloat(r.PostFormValue("max_points"))
	if err != nil {
		return err
	}
	return h.svc.SaveHomework(ctx, code, id, service.HomeworkInput{
		SubjectID:     subjectID,
		Title:         r.PostFormValue("title"),
		DescriptionMD: r.PostFormValue("description_md"),
		DueAt:         due,
		MaxPoints:     maxPoints,
	})
}

func (h *Handler) deleteHomework(ctx context.Context, code string, id int64, _ *http.Request) error {
	return h.svc.DeleteHomework(ctx, code, id)
}

func (h *Handler) addHomeworkLink(ctx context.Context, code string, id int64, r *http.Request) error {
	return h.svc.AddHomeworkLink(ctx, code, id, service.HomeworkLinkInput{
		Title: r.PostFormValue("title"),
		URL:   r.PostFormValue("url"),
	})
}

func (h *Handler) deleteHomeworkLink(ctx context.Context, code string, id int64, _ *http.Request) error {
	return h.svc.DeleteHomeworkLink(ctx, code, id)
}

func (h *Handler) saveNote(ctx context.Context, code string, id int64, r *http.Request) error {
	return h.svc.SaveNote(ctx, code, id, service.NoteInput{
		Title:  r.PostFormValue("title"),
		BodyMD: r.PostFormValue("body_md"),
		Pinned: r.PostFormValue("pinned") == "on",
	})
}

func (h *Handler) deleteNote(ctx context.Context, code string, id int64, _ *http.Request) error {
	return h.svc.DeleteNote(ctx, code, id)
}

func (h *Handler) saveResource(ctx context.Context, code string, id int64, r *http.Request) error {
	subjectID, err := formID(r, "subject_id")
	if err != nil {
		return err
	}
	date, err := parseOptionalDate(r.PostFormValue("date"))
	if err != nil {
		return err
	}
	return h.svc.SaveResource(ctx, code, id, service.ResourceInput{
		SubjectID: subjectID,
		Kind:      r.PostFormValue("kind"),
		Title:     r.PostFormValue("title"),
		URL:       r.PostFormValue("url"),
		Date:      date,
	})
}

func (h *Handler) deleteResource(ctx context.Context, code string, id int64, _ *http.Request) error {
	return h.svc.DeleteResource(ctx, code, id)
}
