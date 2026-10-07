package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// section is one kind of group content with list, create, edit and delete
// pages under /g/{code}/{name}. The list page also holds the create form for
// leaders; the edit page reuses that form.
type section struct {
	name string // URL segment and template name
	// load fills the data the page needs (the list, the subject picker).
	load func(ctx context.Context, h *Handler, groupID int64, d *pageData) error
	// fields returns an existing item as form values for the edit page.
	fields func(ctx context.Context, h *Handler, groupID, id int64) (map[string]string, error)
	create func(ctx context.Context, h *Handler, groupID int64, r *http.Request) error
	update func(ctx context.Context, h *Handler, groupID, id int64, r *http.Request) error
	delete func(ctx context.Context, h *Handler, groupID, id int64) error
}

func (h *Handler) sections() []*section {
	return []*section{
		{
			name: "subjects",
			load: loadSubjects,
			fields: func(ctx context.Context, h *Handler, groupID, id int64) (map[string]string, error) {
				s, err := h.svc.Subject(ctx, groupID, id)
				if err != nil {
					return nil, err
				}
				return map[string]string{"name": s.Name, "short_name": s.ShortName}, nil
			},
			create: func(ctx context.Context, h *Handler, groupID int64, r *http.Request) error {
				_, err := h.svc.CreateSubject(ctx, groupID, subjectInput(r))
				return err
			},
			update: func(ctx context.Context, h *Handler, groupID, id int64, r *http.Request) error {
				_, err := h.svc.UpdateSubject(ctx, groupID, id, subjectInput(r))
				return err
			},
			delete: func(ctx context.Context, h *Handler, groupID, id int64) error {
				return h.svc.DeleteSubject(ctx, groupID, id)
			},
		},
		{
			name: "links",
			load: func(ctx context.Context, h *Handler, groupID int64, d *pageData) error {
				var err error
				if d.ClassLinks, err = h.svc.ClassLinks(ctx, groupID); err != nil {
					return err
				}
				return loadSubjects(ctx, h, groupID, d)
			},
			fields: func(ctx context.Context, h *Handler, groupID, id int64) (map[string]string, error) {
				l, err := h.svc.ClassLink(ctx, groupID, id)
				if err != nil {
					return nil, err
				}
				return map[string]string{
					"subject_id": strconv.FormatInt(l.SubjectID, 10), "lesson_type": string(l.LessonType),
					"url": l.URL, "note": l.Note,
				}, nil
			},
			create: func(ctx context.Context, h *Handler, groupID int64, r *http.Request) error {
				in, err := classLinkInput(r)
				if err == nil {
					_, err = h.svc.CreateClassLink(ctx, groupID, in)
				}
				return err
			},
			update: func(ctx context.Context, h *Handler, groupID, id int64, r *http.Request) error {
				in, err := classLinkInput(r)
				if err == nil {
					_, err = h.svc.UpdateClassLink(ctx, groupID, id, in)
				}
				return err
			},
			delete: func(ctx context.Context, h *Handler, groupID, id int64) error {
				return h.svc.DeleteClassLink(ctx, groupID, id)
			},
		},
		{
			name: "homework",
			load: func(ctx context.Context, h *Handler, groupID int64, d *pageData) error {
				var err error
				d.HomeworkFilter = parseHomeworkFilter(d.Query)
				if d.HomeworkList, err = h.svc.HomeworkList(ctx, groupID, d.HomeworkFilter); err != nil {
					return err
				}
				return loadSubjects(ctx, h, groupID, d)
			},
			fields: func(ctx context.Context, h *Handler, groupID, id int64) (map[string]string, error) {
				hw, err := h.svc.Homework(ctx, groupID, id)
				if err != nil {
					return nil, err
				}
				f := map[string]string{
					"subject_id": strconv.FormatInt(hw.SubjectID, 10), "title": hw.Title,
					"description": hw.DescriptionMD, "links": formatLinks(hw.Links),
				}
				if hw.DueAt != nil {
					f["due_at"] = hw.DueAt.In(h.loc).Format(dateTimeLocal)
				}
				if hw.MaxPoints != nil {
					f["max_points"] = formatPoints(*hw.MaxPoints)
				}
				return f, nil
			},
			create: func(ctx context.Context, h *Handler, groupID int64, r *http.Request) error {
				in, err := h.homeworkInput(r)
				if err == nil {
					_, err = h.svc.CreateHomework(ctx, groupID, in)
				}
				return err
			},
			update: func(ctx context.Context, h *Handler, groupID, id int64, r *http.Request) error {
				in, err := h.homeworkInput(r)
				if err == nil {
					_, err = h.svc.UpdateHomework(ctx, groupID, id, in)
				}
				return err
			},
			delete: func(ctx context.Context, h *Handler, groupID, id int64) error {
				return h.svc.DeleteHomework(ctx, groupID, id)
			},
		},
		{
			name: "notes",
			load: func(ctx context.Context, h *Handler, groupID int64, d *pageData) error {
				var err error
				d.Notes, err = h.svc.Notes(ctx, groupID)
				return err
			},
			fields: func(ctx context.Context, h *Handler, groupID, id int64) (map[string]string, error) {
				n, err := h.svc.Note(ctx, groupID, id)
				if err != nil {
					return nil, err
				}
				f := map[string]string{"title": n.Title, "body": n.BodyMD}
				if n.Pinned {
					f["pinned"] = "on"
				}
				return f, nil
			},
			create: func(ctx context.Context, h *Handler, groupID int64, r *http.Request) error {
				_, err := h.svc.CreateNote(ctx, groupID, noteInput(r))
				return err
			},
			update: func(ctx context.Context, h *Handler, groupID, id int64, r *http.Request) error {
				_, err := h.svc.UpdateNote(ctx, groupID, id, noteInput(r))
				return err
			},
			delete: func(ctx context.Context, h *Handler, groupID, id int64) error {
				return h.svc.DeleteNote(ctx, groupID, id)
			},
		},
		{
			name: "resources",
			load: func(ctx context.Context, h *Handler, groupID int64, d *pageData) error {
				var err error
				if d.Resources, err = h.svc.ResourceLinks(ctx, groupID); err != nil {
					return err
				}
				return loadSubjects(ctx, h, groupID, d)
			},
			fields: func(ctx context.Context, h *Handler, groupID, id int64) (map[string]string, error) {
				l, err := h.svc.ResourceLink(ctx, groupID, id)
				if err != nil {
					return nil, err
				}
				return map[string]string{
					"subject_id": strconv.FormatInt(l.SubjectID, 10), "kind": string(l.Kind),
					"title": l.Title, "url": l.URL, "date": l.Date,
				}, nil
			},
			create: func(ctx context.Context, h *Handler, groupID int64, r *http.Request) error {
				in, err := resourceInput(r)
				if err == nil {
					_, err = h.svc.CreateResourceLink(ctx, groupID, in)
				}
				return err
			},
			update: func(ctx context.Context, h *Handler, groupID, id int64, r *http.Request) error {
				in, err := resourceInput(r)
				if err == nil {
					_, err = h.svc.UpdateResourceLink(ctx, groupID, id, in)
				}
				return err
			},
			delete: func(ctx context.Context, h *Handler, groupID, id int64) error {
				return h.svc.DeleteResourceLink(ctx, groupID, id)
			},
		},
	}
}

// parseHomeworkFilter reads the homework list filter from the query string
// (?subject_id=…&status=…). Values that are not an id or a status are
// ignored, so a stale or edited link shows the whole list.
func parseHomeworkFilter(q url.Values) service.HomeworkFilter {
	var f service.HomeworkFilter
	if id, err := strconv.ParseInt(q.Get("subject_id"), 10, 64); err == nil && id > 0 {
		f.SubjectID = id
	}
	for _, st := range service.Statuses {
		if q.Get("status") == string(st) {
			f.Status = st
		}
	}
	return f
}

// homeworkFilterQuery encodes f for the homework list URL, without the "?".
func homeworkFilterQuery(f service.HomeworkFilter) string {
	q := url.Values{}
	if f.SubjectID != 0 {
		q.Set("subject_id", strconv.FormatInt(f.SubjectID, 10))
	}
	if f.Status != "" {
		q.Set("status", string(f.Status))
	}
	return q.Encode()
}

func loadSubjects(ctx context.Context, h *Handler, groupID int64, d *pageData) error {
	var err error
	d.Subjects, err = h.svc.Subjects(ctx, groupID)
	return err
}

// registerGroupRoutes adds the group pages to mux.
func (h *Handler) registerGroupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /g/{code}", h.groupOverview)
	mux.HandleFunc("GET /g/{code}/homework/{id}", h.homeworkDetail)
	mux.HandleFunc("POST /g/{code}/homework/{id}/progress", h.updateProgress)
	mux.HandleFunc("GET /g/{code}/grades", h.myGrades)
	mux.HandleFunc("GET /g/{code}/schedule", h.schedulePage)
	mux.HandleFunc("POST /g/{code}/schedule/sync", h.syncSchedule)
	for _, s := range h.sections() {
		base := "/g/{code}/" + s.name
		mux.HandleFunc("GET "+base, h.sectionList(s))
		mux.HandleFunc("POST "+base, h.sectionCreate(s))
		mux.HandleFunc("GET "+base+"/{id}/edit", h.sectionEdit(s))
		mux.HandleFunc("POST "+base+"/{id}", h.sectionUpdate(s))
		mux.HandleFunc("POST "+base+"/{id}/delete", h.sectionDelete(s))
	}
}

// groupPage resolves the group in the URL, or renders the error and returns nil.
func (h *Handler) groupPage(w http.ResponseWriter, r *http.Request) *service.GroupView {
	g, err := h.svc.Group(r.Context(), r.PathValue("code"))
	if err != nil {
		h.renderError(w, r, err)
		return nil
	}
	return g
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, service.ErrNotFound
	}
	return id, nil
}

func sectionURL(g *service.GroupView, name string) string {
	return "/g/" + g.Code + "/" + name
}

// showSection renders a section page: the list with the create form, or the
// edit form when editID is set.
func (h *Handler) showSection(w http.ResponseWriter, r *http.Request, s *section, g *service.GroupView, status int, d pageData) {
	d.Group = g
	d.Section = s.name
	d.Query = r.URL.Query()
	if d.Fields == nil {
		d.Fields = map[string]string{}
	}
	if err := s.load(r.Context(), h, g.ID, &d); err != nil {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, status, s.name, d)
}

func (h *Handler) sectionList(s *section) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g := h.groupPage(w, r); g != nil {
			h.showSection(w, r, s, g, http.StatusOK, pageData{})
		}
	}
}

func (h *Handler) sectionCreate(s *section) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g := h.groupPage(w, r)
		if g == nil {
			return
		}
		if err := s.create(r.Context(), h, g.ID, r); err != nil {
			h.sectionFormError(w, r, s, g, 0, err)
			return
		}
		http.Redirect(w, r, sectionURL(g, s.name), http.StatusSeeOther)
	}
}

func (h *Handler) sectionEdit(s *section) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g := h.groupPage(w, r)
		if g == nil {
			return
		}
		if !g.CanManage {
			h.renderError(w, r, service.ErrForbidden)
			return
		}
		id, err := pathID(r)
		if err != nil {
			h.renderError(w, r, err)
			return
		}
		fields, err := s.fields(r.Context(), h, g.ID, id)
		if err != nil {
			h.renderError(w, r, err)
			return
		}
		h.showSection(w, r, s, g, http.StatusOK, pageData{EditID: id, Fields: fields})
	}
}

func (h *Handler) sectionUpdate(s *section) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g := h.groupPage(w, r)
		if g == nil {
			return
		}
		id, err := pathID(r)
		if err == nil {
			err = s.update(r.Context(), h, g.ID, id, r)
		}
		if err != nil {
			h.sectionFormError(w, r, s, g, id, err)
			return
		}
		http.Redirect(w, r, sectionURL(g, s.name), http.StatusSeeOther)
	}
}

func (h *Handler) sectionDelete(s *section) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g := h.groupPage(w, r)
		if g == nil {
			return
		}
		id, err := pathID(r)
		if err == nil {
			err = s.delete(r.Context(), h, g.ID, id)
		}
		if errors.Is(err, service.ErrSubjectInUse) {
			msg, status, _ := userMessage(r, err)
			h.showSection(w, r, s, g, status, pageData{Error: msg})
			return
		}
		if err != nil {
			h.renderError(w, r, err)
			return
		}
		http.Redirect(w, r, sectionURL(g, s.name), http.StatusSeeOther)
	}
}

// sectionFormError shows the form again with what the user typed and the
// error, or the error page for errors the user cannot fix in the form.
func (h *Handler) sectionFormError(w http.ResponseWriter, r *http.Request, s *section, g *service.GroupView, editID int64, err error) {
	msg, status, ok := userMessage(r, err)
	if !ok {
		h.renderError(w, r, err)
		return
	}
	fields := map[string]string{}
	for k, v := range r.PostForm {
		if k != "csrf_token" && len(v) > 0 {
			fields[k] = v[0]
		}
	}
	h.showSection(w, r, s, g, status, pageData{EditID: editID, Fields: fields, Error: msg})
}

func (h *Handler) groupOverview(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	ov, err := h.svc.GroupOverview(r.Context(), g.ID)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "group", pageData{
		Group: g, Section: "overview", HomeworkList: ov.Homework, Notes: ov.Notes, ClassLinks: ov.ClassLinks,
		Schedule: ov.Today,
	})
}

func (h *Handler) homeworkDetail(w http.ResponseWriter, r *http.Request) {
	g := h.groupPage(w, r)
	if g == nil {
		return
	}
	id, err := pathID(r)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	hw, err := h.svc.Homework(r.Context(), g.ID, id)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "homework_detail", pageData{Group: g, Section: "homework", Homework: hw})
}

// Form parsing. Values the service validates are passed through as typed;
// only values that need converting are checked here.

func subjectInput(r *http.Request) service.SubjectInput {
	return service.SubjectInput{Name: r.PostFormValue("name"), ShortName: r.PostFormValue("short_name")}
}

func formSubjectID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PostFormValue("subject_id"), 10, 64)
	if err != nil {
		return 0, &service.InputError{Field: "subject_id", Msg: i18n.M("err.choose_subject")}
	}
	return id, nil
}

func classLinkInput(r *http.Request) (service.ClassLinkInput, error) {
	subjectID, err := formSubjectID(r)
	return service.ClassLinkInput{
		SubjectID:  subjectID,
		LessonType: store.LessonType(r.PostFormValue("lesson_type")),
		URL:        r.PostFormValue("url"),
		Note:       r.PostFormValue("note"),
	}, err
}

func noteInput(r *http.Request) service.NoteInput {
	return service.NoteInput{
		Title:  r.PostFormValue("title"),
		Body:   r.PostFormValue("body"),
		Pinned: r.PostFormValue("pinned") != "",
	}
}

func resourceInput(r *http.Request) (service.ResourceLinkInput, error) {
	subjectID, err := formSubjectID(r)
	return service.ResourceLinkInput{
		SubjectID: subjectID,
		Kind:      store.ResourceKind(r.PostFormValue("kind")),
		Title:     r.PostFormValue("title"),
		URL:       r.PostFormValue("url"),
		Date:      r.PostFormValue("date"),
	}, err
}

// dateTimeLocal is the value format of <input type="datetime-local">.
const dateTimeLocal = "2006-01-02T15:04"

func (h *Handler) homeworkInput(r *http.Request) (service.HomeworkInput, error) {
	in := service.HomeworkInput{
		Title:       r.PostFormValue("title"),
		Description: r.PostFormValue("description"),
		Links:       parseLinks(r.PostFormValue("links")),
	}
	var err error
	if in.SubjectID, err = formSubjectID(r); err != nil {
		return in, err
	}
	if s := strings.TrimSpace(r.PostFormValue("due_at")); s != "" {
		t, err := time.ParseInLocation(dateTimeLocal, s, h.loc)
		if err != nil {
			return in, &service.InputError{Field: "due_at", Msg: i18n.M("err.due_invalid")}
		}
		in.DueAt = &t
	}
	if s := strings.TrimSpace(r.PostFormValue("max_points")); s != "" {
		p, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
		if err != nil {
			return in, &service.InputError{Field: "max_points", Msg: i18n.M("err.max_points_nan")}
		}
		in.MaxPoints = &p
	}
	return in, nil
}

// parseLinks reads one link per line: an address, optionally preceded by a
// title ("Lab manual — https://…"). One separator standing on its own before
// the address is dropped, so what formatLinks writes reads back unchanged.
// Blank lines are skipped.
func parseLinks(s string) []service.LinkInput {
	var out []service.LinkInput
	for line := range strings.Lines(s) {
		line = strings.TrimSpace(line)
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		u := f[len(f)-1]
		title := strings.TrimSpace(strings.TrimSuffix(line, u))
		for _, sep := range linkSeparators {
			if title == sep {
				title = ""
				break
			}
			if t, ok := strings.CutSuffix(title, " "+sep); ok {
				title = strings.TrimSpace(t)
				break
			}
		}
		out = append(out, service.LinkInput{Title: title, URL: u})
	}
	return out
}

var linkSeparators = []string{"—", "–", "-", "|"}

func formatLinks(links []*store.HomeworkLink) string {
	var b strings.Builder
	for _, l := range links {
		if l.Title != "" {
			b.WriteString(l.Title + " — ")
		}
		b.WriteString(l.URL + "\n")
	}
	return b.String()
}

func formatPoints(p float64) string {
	return strconv.FormatFloat(p, 'f', -1, 64)
}
