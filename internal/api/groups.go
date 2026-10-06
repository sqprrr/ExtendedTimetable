package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/markdown"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Group content endpoints. Lists and items are readable by the group's
// members; POST, PUT and DELETE need a leader (the service checks both).
func (h *Handler) registerGroupRoutes(mux *http.ServeMux) {
	const g = "/api/v1/groups/{code}"
	mux.HandleFunc("GET "+g+"/subjects", h.listSubjects)
	mux.HandleFunc("POST "+g+"/subjects", h.createSubject)
	mux.HandleFunc("PUT "+g+"/subjects/{id}", h.updateSubject)
	mux.HandleFunc("DELETE "+g+"/subjects/{id}", h.deleteSubject)

	mux.HandleFunc("GET "+g+"/class-links", h.listClassLinks)
	mux.HandleFunc("POST "+g+"/class-links", h.createClassLink)
	mux.HandleFunc("PUT "+g+"/class-links/{id}", h.updateClassLink)
	mux.HandleFunc("DELETE "+g+"/class-links/{id}", h.deleteClassLink)

	mux.HandleFunc("GET "+g+"/homework", h.listHomework)
	mux.HandleFunc("GET "+g+"/homework/{id}", h.getHomework)
	mux.HandleFunc("POST "+g+"/homework", h.createHomework)
	mux.HandleFunc("PUT "+g+"/homework/{id}", h.updateHomework)
	mux.HandleFunc("DELETE "+g+"/homework/{id}", h.deleteHomework)

	mux.HandleFunc("GET "+g+"/notes", h.listNotes)
	mux.HandleFunc("POST "+g+"/notes", h.createNote)
	mux.HandleFunc("PUT "+g+"/notes/{id}", h.updateNote)
	mux.HandleFunc("DELETE "+g+"/notes/{id}", h.deleteNote)

	mux.HandleFunc("GET "+g+"/resources", h.listResources)
	mux.HandleFunc("POST "+g+"/resources", h.createResource)
	mux.HandleFunc("PUT "+g+"/resources/{id}", h.updateResource)
	mux.HandleFunc("DELETE "+g+"/resources/{id}", h.deleteResource)
}

// group resolves {code}, or writes the error and returns 0.
func (h *Handler) group(w http.ResponseWriter, r *http.Request) int64 {
	g, err := h.svc.Group(r.Context(), r.PathValue("code"))
	if err != nil {
		h.fail(w, r, err)
		return 0
	}
	return g.ID
}

// groupAndID resolves {code} and {id}, or writes the error and returns ok=false.
func (h *Handler) groupAndID(w http.ResponseWriter, r *http.Request) (groupID, id int64, ok bool) {
	if groupID = h.group(w, r); groupID == 0 {
		return 0, 0, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		h.fail(w, r, service.ErrNotFound)
		return 0, 0, false
	}
	return groupID, id, true
}

const maxBodyBytes = 1 << 20

// decode reads a JSON request body into v, or writes a 400 and returns false.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// write sends v, or the error when err is set.
func (h *Handler) write(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if v == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, status, v)
}

func list[T, J any](items []T, conv func(T) J) []J {
	out := make([]J, 0, len(items))
	for _, it := range items {
		out = append(out, conv(it))
	}
	return out
}

// Subjects

type subjectJSON struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

type subjectRequest struct {
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

func toSubject(s *store.Subject) subjectJSON {
	return subjectJSON{ID: s.ID, Name: s.Name, ShortName: s.ShortName}
}

func (h *Handler) listSubjects(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		subs, err := h.svc.Subjects(r.Context(), gid)
		h.write(w, r, http.StatusOK, list(subs, toSubject), err)
	}
}

func (h *Handler) createSubject(w http.ResponseWriter, r *http.Request) {
	var req subjectRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		s, err := h.svc.CreateSubject(r.Context(), gid, service.SubjectInput(req))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, toSubject(s))
	}
}

func (h *Handler) updateSubject(w http.ResponseWriter, r *http.Request) {
	var req subjectRequest
	if gid, id, ok := h.groupAndID(w, r); ok && decode(w, r, &req) {
		s, err := h.svc.UpdateSubject(r.Context(), gid, id, service.SubjectInput(req))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toSubject(s))
	}
}

func (h *Handler) deleteSubject(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.write(w, r, 0, nil, h.svc.DeleteSubject(r.Context(), gid, id))
	}
}

// Class links

type classLinkJSON struct {
	ID          int64            `json:"id"`
	SubjectID   int64            `json:"subject_id"`
	SubjectName string           `json:"subject_name,omitempty"`
	LessonType  store.LessonType `json:"lesson_type"`
	URL         string           `json:"url"`
	Note        string           `json:"note"`
}

type classLinkRequest struct {
	SubjectID  int64            `json:"subject_id"`
	LessonType store.LessonType `json:"lesson_type"`
	URL        string           `json:"url"`
	Note       string           `json:"note"`
}

func toClassLink(l *store.ClassLink) classLinkJSON {
	return classLinkJSON{ID: l.ID, SubjectID: l.SubjectID, LessonType: l.LessonType, URL: l.URL, Note: l.Note}
}

func (h *Handler) listClassLinks(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		links, err := h.svc.ClassLinks(r.Context(), gid)
		h.write(w, r, http.StatusOK, list(links, func(l *service.ClassLink) classLinkJSON {
			j := toClassLink(&l.ClassLink)
			j.SubjectName = l.SubjectName
			return j
		}), err)
	}
}

func (h *Handler) createClassLink(w http.ResponseWriter, r *http.Request) {
	var req classLinkRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		l, err := h.svc.CreateClassLink(r.Context(), gid, service.ClassLinkInput(req))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, toClassLink(l))
	}
}

func (h *Handler) updateClassLink(w http.ResponseWriter, r *http.Request) {
	var req classLinkRequest
	if gid, id, ok := h.groupAndID(w, r); ok && decode(w, r, &req) {
		l, err := h.svc.UpdateClassLink(r.Context(), gid, id, service.ClassLinkInput(req))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toClassLink(l))
	}
}

func (h *Handler) deleteClassLink(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.write(w, r, 0, nil, h.svc.DeleteClassLink(r.Context(), gid, id))
	}
}

// Homework

type linkJSON struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type homeworkJSON struct {
	ID              int64      `json:"id"`
	SubjectID       int64      `json:"subject_id"`
	SubjectName     string     `json:"subject_name,omitempty"`
	Title           string     `json:"title"`
	DescriptionMD   string     `json:"description_md"`
	DescriptionHTML string     `json:"description_html,omitempty"`
	DueAt           *time.Time `json:"due_at"`
	MaxPoints       *float64   `json:"max_points"`
	Overdue         bool       `json:"overdue"`
	Links           []linkJSON `json:"links,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type homeworkRequest struct {
	SubjectID     int64      `json:"subject_id"`
	Title         string     `json:"title"`
	DescriptionMD string     `json:"description_md"`
	DueAt         *time.Time `json:"due_at"`
	MaxPoints     *float64   `json:"max_points"`
	Links         []linkJSON `json:"links"`
}

func (req homeworkRequest) input() service.HomeworkInput {
	return service.HomeworkInput{
		SubjectID: req.SubjectID, Title: req.Title, Description: req.DescriptionMD,
		DueAt: req.DueAt, MaxPoints: req.MaxPoints,
		Links: list(req.Links, func(l linkJSON) service.LinkInput { return service.LinkInput(l) }),
	}
}

func toHomework(hw *store.Homework) homeworkJSON {
	return homeworkJSON{
		ID: hw.ID, SubjectID: hw.SubjectID, Title: hw.Title, DescriptionMD: hw.DescriptionMD,
		DueAt: hw.DueAt, MaxPoints: hw.MaxPoints, CreatedAt: hw.CreatedAt, UpdatedAt: hw.UpdatedAt,
	}
}

func toHomeworkView(hw *service.Homework) homeworkJSON {
	j := toHomework(&hw.Homework)
	j.SubjectName, j.Overdue = hw.SubjectName, hw.Overdue
	return j
}

func (h *Handler) listHomework(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		hws, err := h.svc.HomeworkList(r.Context(), gid)
		h.write(w, r, http.StatusOK, list(hws, toHomeworkView), err)
	}
}

func (h *Handler) getHomework(w http.ResponseWriter, r *http.Request) {
	gid, id, ok := h.groupAndID(w, r)
	if !ok {
		return
	}
	hw, err := h.svc.Homework(r.Context(), gid, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	j := toHomeworkView(hw)
	j.DescriptionHTML = markdown.ToHTML(hw.DescriptionMD)
	j.Links = list(hw.Links, func(l *store.HomeworkLink) linkJSON { return linkJSON{Title: l.Title, URL: l.URL} })
	writeJSON(w, http.StatusOK, j)
}

func (h *Handler) createHomework(w http.ResponseWriter, r *http.Request) {
	var req homeworkRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		hw, err := h.svc.CreateHomework(r.Context(), gid, req.input())
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, toHomework(hw))
	}
}

func (h *Handler) updateHomework(w http.ResponseWriter, r *http.Request) {
	var req homeworkRequest
	if gid, id, ok := h.groupAndID(w, r); ok && decode(w, r, &req) {
		hw, err := h.svc.UpdateHomework(r.Context(), gid, id, req.input())
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toHomework(hw))
	}
}

func (h *Handler) deleteHomework(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.write(w, r, 0, nil, h.svc.DeleteHomework(r.Context(), gid, id))
	}
}

// Notes

type noteJSON struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	BodyMD    string    `json:"body_md"`
	BodyHTML  string    `json:"body_html"`
	Pinned    bool      `json:"pinned"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type noteRequest struct {
	Title  string `json:"title"`
	BodyMD string `json:"body_md"`
	Pinned bool   `json:"pinned"`
}

func toNote(n *store.Note) noteJSON {
	return noteJSON{
		ID: n.ID, Title: n.Title, BodyMD: n.BodyMD, BodyHTML: markdown.ToHTML(n.BodyMD), Pinned: n.Pinned,
		CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
	}
}

func (h *Handler) listNotes(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		notes, err := h.svc.Notes(r.Context(), gid)
		h.write(w, r, http.StatusOK, list(notes, toNote), err)
	}
}

func (h *Handler) createNote(w http.ResponseWriter, r *http.Request) {
	var req noteRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		n, err := h.svc.CreateNote(r.Context(), gid, service.NoteInput{Title: req.Title, Body: req.BodyMD, Pinned: req.Pinned})
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, toNote(n))
	}
}

func (h *Handler) updateNote(w http.ResponseWriter, r *http.Request) {
	var req noteRequest
	if gid, id, ok := h.groupAndID(w, r); ok && decode(w, r, &req) {
		n, err := h.svc.UpdateNote(r.Context(), gid, id, service.NoteInput{Title: req.Title, Body: req.BodyMD, Pinned: req.Pinned})
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toNote(n))
	}
}

func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.write(w, r, 0, nil, h.svc.DeleteNote(r.Context(), gid, id))
	}
}

// Recordings and solutions

type resourceJSON struct {
	ID          int64              `json:"id"`
	SubjectID   int64              `json:"subject_id"`
	SubjectName string             `json:"subject_name,omitempty"`
	Kind        store.ResourceKind `json:"kind"`
	Title       string             `json:"title"`
	URL         string             `json:"url"`
	Date        string             `json:"date,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
}

type resourceRequest struct {
	SubjectID int64              `json:"subject_id"`
	Kind      store.ResourceKind `json:"kind"`
	Title     string             `json:"title"`
	URL       string             `json:"url"`
	Date      string             `json:"date"`
}

func toResource(l *store.ResourceLink) resourceJSON {
	return resourceJSON{
		ID: l.ID, SubjectID: l.SubjectID, Kind: l.Kind, Title: l.Title, URL: l.URL, Date: l.Date, CreatedAt: l.CreatedAt,
	}
}

func (h *Handler) listResources(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		links, err := h.svc.ResourceLinks(r.Context(), gid)
		h.write(w, r, http.StatusOK, list(links, func(l *service.ResourceLink) resourceJSON {
			j := toResource(&l.ResourceLink)
			j.SubjectName = l.SubjectName
			return j
		}), err)
	}
}

func (h *Handler) createResource(w http.ResponseWriter, r *http.Request) {
	var req resourceRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		l, err := h.svc.CreateResourceLink(r.Context(), gid, service.ResourceLinkInput(req))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, toResource(l))
	}
}

func (h *Handler) updateResource(w http.ResponseWriter, r *http.Request) {
	var req resourceRequest
	if gid, id, ok := h.groupAndID(w, r); ok && decode(w, r, &req) {
		l, err := h.svc.UpdateResourceLink(r.Context(), gid, id, service.ResourceLinkInput(req))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toResource(l))
	}
}

func (h *Handler) deleteResource(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.write(w, r, 0, nil, h.svc.DeleteResourceLink(r.Context(), gid, id))
	}
}
