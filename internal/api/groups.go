package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/markdown"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Group content endpoints. Lists and items are readable by the group's
// members; POST, PUT and DELETE need the leader or an editor (the service
// checks both).
//
// PUT updates only the fields present in the body: the body is decoded over
// the current item. Send null to clear an optional field (due_at, max_points)
// and [] to remove all homework links.
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
	mux.HandleFunc("PUT "+g+"/homework/{id}/progress", h.updateProgress)
	mux.HandleFunc("GET "+g+"/schedule", h.schedule)
	mux.HandleFunc("POST "+g+"/schedule/sync", h.syncSchedule)

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

// reply sends conv(v) with status, or the error when err is set.
func reply[T, J any](h *Handler, w http.ResponseWriter, r *http.Request, status int, v T, err error, conv func(T) J) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, status, conv(v))
}

// deleted sends 204, or the error when err is set.
func (h *Handler) deleted(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	// Hue is the colour the subject is shown in (the default one when none
	// was chosen).
	Hue        string `json:"hue"`
	Lecturer   string `json:"lecturer"`
	Instructor string `json:"instructor"`
	DLURL      string `json:"dl_url"`
}

type subjectRequest struct {
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	// Hue is one of the eight hues, or "" for the default.
	Hue string `json:"hue"`
	// Lecturer, Instructor (practice classes and labs) and DLURL (the
	// distance-learning page) may be "".
	Lecturer   string `json:"lecturer"`
	Instructor string `json:"instructor"`
	DLURL      string `json:"dl_url"`
}

func toSubject(s *store.Subject) subjectJSON {
	return subjectJSON{
		ID: s.ID, Name: s.Name, ShortName: s.ShortName, Hue: service.Hue(s.ID, s.Hue),
		Lecturer: s.Lecturer, Instructor: s.Instructor, DLURL: s.DLURL,
	}
}

func (h *Handler) listSubjects(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		subs, err := h.svc.Subjects(r.Context(), gid)
		reply(h, w, r, http.StatusOK, subs, err, func(s []*store.Subject) []subjectJSON { return list(s, toSubject) })
	}
}

func (h *Handler) createSubject(w http.ResponseWriter, r *http.Request) {
	var req subjectRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		s, err := h.svc.CreateSubject(r.Context(), gid, service.SubjectInput(req))
		reply(h, w, r, http.StatusCreated, s, err, toSubject)
	}
}

func (h *Handler) updateSubject(w http.ResponseWriter, r *http.Request) {
	gid, id, ok := h.groupAndID(w, r)
	if !ok {
		return
	}
	cur, err := h.svc.Subject(r.Context(), gid, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	req := subjectRequest{
		Name: cur.Name, ShortName: cur.ShortName, Hue: cur.Hue,
		Lecturer: cur.Lecturer, Instructor: cur.Instructor, DLURL: cur.DLURL,
	}
	if decode(w, r, &req) {
		s, err := h.svc.UpdateSubject(r.Context(), gid, id, service.SubjectInput(req))
		reply(h, w, r, http.StatusOK, s, err, toSubject)
	}
}

func (h *Handler) deleteSubject(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.deleted(w, r, h.svc.DeleteSubject(r.Context(), gid, id))
	}
}

// Class links

type classLinkJSON struct {
	ID          int64            `json:"id"`
	SubjectID   int64            `json:"subject_id"`
	SubjectName string           `json:"subject_name"`
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
	return classLinkJSON{
		ID: l.ID, SubjectID: l.SubjectID, SubjectName: l.SubjectName, LessonType: l.LessonType, URL: l.URL, Note: l.Note,
	}
}

func (h *Handler) listClassLinks(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		links, err := h.svc.ClassLinks(r.Context(), gid)
		reply(h, w, r, http.StatusOK, links, err, func(l []*store.ClassLink) []classLinkJSON { return list(l, toClassLink) })
	}
}

func (h *Handler) createClassLink(w http.ResponseWriter, r *http.Request) {
	var req classLinkRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		l, err := h.svc.CreateClassLink(r.Context(), gid, service.ClassLinkInput(req))
		reply(h, w, r, http.StatusCreated, l, err, toClassLink)
	}
}

func (h *Handler) updateClassLink(w http.ResponseWriter, r *http.Request) {
	gid, id, ok := h.groupAndID(w, r)
	if !ok {
		return
	}
	cur, err := h.svc.ClassLink(r.Context(), gid, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	req := classLinkRequest{SubjectID: cur.SubjectID, LessonType: cur.LessonType, URL: cur.URL, Note: cur.Note}
	if decode(w, r, &req) {
		l, err := h.svc.UpdateClassLink(r.Context(), gid, id, service.ClassLinkInput(req))
		reply(h, w, r, http.StatusOK, l, err, toClassLink)
	}
}

func (h *Handler) deleteClassLink(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.deleted(w, r, h.svc.DeleteClassLink(r.Context(), gid, id))
	}
}

// Homework

type linkJSON struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type homeworkJSON struct {
	ID            int64  `json:"id"`
	SubjectID     int64  `json:"subject_id"`
	SubjectName   string `json:"subject_name"`
	Title         string `json:"title"`
	DescriptionMD string `json:"description_md"`
	// DescriptionHTML and Links are left out of lists.
	DescriptionHTML string     `json:"description_html,omitempty"`
	Links           []linkJSON `json:"links,omitempty"`
	DueAt           *time.Time `json:"due_at"`
	MaxPoints       *float64   `json:"max_points"`
	Overdue         bool       `json:"overdue"`
	// Progress is the viewer's own status and grade; absent when the viewer
	// has no tracker in the group (a superadmin who is not a member).
	Progress  *progressJSON `json:"progress,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type progressJSON struct {
	Status store.ProgressStatus `json:"status"`
	Grade  *float64             `json:"grade"`
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

func toLinks(links []*store.HomeworkLink) []linkJSON {
	return list(links, func(l *store.HomeworkLink) linkJSON { return linkJSON{Title: l.Title, URL: l.URL} })
}

// toHomeworkSummary converts an assignment for a list.
func toHomeworkSummary(hw *service.Homework) homeworkJSON {
	j := homeworkJSON{
		ID: hw.ID, SubjectID: hw.SubjectID, SubjectName: hw.SubjectName, Title: hw.Title,
		DescriptionMD: hw.DescriptionMD, DueAt: hw.DueAt, MaxPoints: hw.MaxPoints, Overdue: hw.Overdue,
		CreatedAt: hw.CreatedAt, UpdatedAt: hw.UpdatedAt,
	}
	if hw.Tracked {
		j.Progress = &progressJSON{Status: hw.Status, Grade: hw.Grade}
	}
	return j
}

// toHomework converts a single assignment, with its links and rendered
// description.
func toHomework(hw *service.Homework) homeworkJSON {
	j := toHomeworkSummary(hw)
	j.DescriptionHTML = markdown.ToHTML(hw.DescriptionMD)
	j.Links = toLinks(hw.Links)
	return j
}

// listHomework lists the group's homework, optionally only that of
// ?subject_id and with the viewer's ?status.
func (h *Handler) listHomework(w http.ResponseWriter, r *http.Request) {
	gid := h.group(w, r)
	if gid == 0 {
		return
	}
	q := r.URL.Query()
	var f service.HomeworkFilter
	if s := q.Get("status"); s != "" {
		if !slices.Contains(service.Statuses, store.ProgressStatus(s)) {
			writeError(w, r, http.StatusBadRequest, "status must be not_started, in_progress or done")
			return
		}
		f.Status = store.ProgressStatus(s)
	}
	if s := q.Get("subject_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, r, http.StatusBadRequest, "subject_id must be a subject id")
			return
		}
		f.SubjectID = id
	}
	hws, err := h.svc.HomeworkList(r.Context(), gid, f)
	reply(h, w, r, http.StatusOK, hws, err, func(hws []*service.Homework) []homeworkJSON { return list(hws, toHomeworkSummary) })
}

func (h *Handler) getHomework(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		hw, err := h.svc.Homework(r.Context(), gid, id)
		reply(h, w, r, http.StatusOK, hw, err, toHomework)
	}
}

func (h *Handler) createHomework(w http.ResponseWriter, r *http.Request) {
	var req homeworkRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		hw, err := h.svc.CreateHomework(r.Context(), gid, req.input())
		reply(h, w, r, http.StatusCreated, hw, err, toHomework)
	}
}

func (h *Handler) updateHomework(w http.ResponseWriter, r *http.Request) {
	gid, id, ok := h.groupAndID(w, r)
	if !ok {
		return
	}
	cur, err := h.svc.Homework(r.Context(), gid, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var body struct {
		homeworkRequest
		// Links is raw so that the links sent replace the current ones:
		// decoded over them, a field left out would keep the old link's value.
		Links json.RawMessage `json:"links"`
	}
	body.homeworkRequest = homeworkRequest{
		SubjectID: cur.SubjectID, Title: cur.Title, DescriptionMD: cur.DescriptionMD,
		DueAt: cur.DueAt, MaxPoints: cur.MaxPoints, Links: toLinks(cur.Links),
	}
	if !decode(w, r, &body) {
		return
	}
	req := body.homeworkRequest
	if len(body.Links) > 0 {
		dec := json.NewDecoder(bytes.NewReader(body.Links))
		dec.DisallowUnknownFields()
		req.Links = nil
		if err := dec.Decode(&req.Links); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid JSON body: links: "+err.Error())
			return
		}
	}
	hw, err := h.svc.UpdateHomework(r.Context(), gid, id, req.input())
	reply(h, w, r, http.StatusOK, hw, err, toHomework)
}

func (h *Handler) deleteHomework(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.deleted(w, r, h.svc.DeleteHomework(r.Context(), gid, id))
	}
}

// progressRequest is the body of PUT …/progress. Only the fields present are
// changed, and the service applies them inside one transaction, so a status
// change never writes back a grade read earlier.
type progressRequest struct {
	Status *store.ProgressStatus `json:"status"`
	// Grade is raw so that a missing field (keep) differs from null (clear).
	Grade json.RawMessage `json:"grade"`
}

// updateProgress sets the viewer's own status and grade.
func (h *Handler) updateProgress(w http.ResponseWriter, r *http.Request) {
	gid, id, ok := h.groupAndID(w, r)
	if !ok {
		return
	}
	var req progressRequest
	if !decode(w, r, &req) {
		return
	}
	in := service.ProgressInput{Status: req.Status}
	if len(req.Grade) > 0 {
		in.SetGrade = true
		if err := json.Unmarshal(req.Grade, &in.Grade); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid JSON body: grade must be a number or null")
			return
		}
	}
	hw, err := h.svc.UpdateProgress(r.Context(), gid, id, in)
	reply(h, w, r, http.StatusOK, hw, err, toHomework)
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

func (req noteRequest) input() service.NoteInput {
	return service.NoteInput{Title: req.Title, Body: req.BodyMD, Pinned: req.Pinned}
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
		reply(h, w, r, http.StatusOK, notes, err, func(n []*store.Note) []noteJSON { return list(n, toNote) })
	}
}

func (h *Handler) createNote(w http.ResponseWriter, r *http.Request) {
	var req noteRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		n, err := h.svc.CreateNote(r.Context(), gid, req.input())
		reply(h, w, r, http.StatusCreated, n, err, toNote)
	}
}

func (h *Handler) updateNote(w http.ResponseWriter, r *http.Request) {
	gid, id, ok := h.groupAndID(w, r)
	if !ok {
		return
	}
	cur, err := h.svc.Note(r.Context(), gid, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	req := noteRequest{Title: cur.Title, BodyMD: cur.BodyMD, Pinned: cur.Pinned}
	if decode(w, r, &req) {
		n, err := h.svc.UpdateNote(r.Context(), gid, id, req.input())
		reply(h, w, r, http.StatusOK, n, err, toNote)
	}
}

func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.deleted(w, r, h.svc.DeleteNote(r.Context(), gid, id))
	}
}

// Recordings and solutions

type resourceJSON struct {
	ID          int64              `json:"id"`
	SubjectID   int64              `json:"subject_id"`
	SubjectName string             `json:"subject_name"`
	Kind        store.ResourceKind `json:"kind"`
	Title       string             `json:"title"`
	URL         string             `json:"url"`
	Date        string             `json:"date,omitempty"`
	LessonType  store.LessonType   `json:"lesson_type,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
}

type resourceRequest struct {
	SubjectID int64              `json:"subject_id"`
	Kind      store.ResourceKind `json:"kind"`
	Title     string             `json:"title"`
	URL       string             `json:"url"`
	Date      string             `json:"date"`
	// LessonType is "lecture", "practice", "lab" or "" for none.
	LessonType store.LessonType `json:"lesson_type"`
}

func toResource(l *store.ResourceLink) resourceJSON {
	return resourceJSON{
		ID: l.ID, SubjectID: l.SubjectID, SubjectName: l.SubjectName, Kind: l.Kind, Title: l.Title, URL: l.URL,
		Date: l.Date, LessonType: l.LessonType, CreatedAt: l.CreatedAt,
	}
}

func (h *Handler) listResources(w http.ResponseWriter, r *http.Request) {
	if gid := h.group(w, r); gid != 0 {
		links, err := h.svc.ResourceLinks(r.Context(), gid)
		reply(h, w, r, http.StatusOK, links, err, func(l []*store.ResourceLink) []resourceJSON { return list(l, toResource) })
	}
}

func (h *Handler) createResource(w http.ResponseWriter, r *http.Request) {
	var req resourceRequest
	if gid := h.group(w, r); gid != 0 && decode(w, r, &req) {
		l, err := h.svc.CreateResourceLink(r.Context(), gid, service.ResourceLinkInput(req))
		reply(h, w, r, http.StatusCreated, l, err, toResource)
	}
}

func (h *Handler) updateResource(w http.ResponseWriter, r *http.Request) {
	gid, id, ok := h.groupAndID(w, r)
	if !ok {
		return
	}
	cur, err := h.svc.ResourceLink(r.Context(), gid, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	req := resourceRequest{
		SubjectID: cur.SubjectID, Kind: cur.Kind, Title: cur.Title, URL: cur.URL, Date: cur.Date, LessonType: cur.LessonType,
	}
	if decode(w, r, &req) {
		l, err := h.svc.UpdateResourceLink(r.Context(), gid, id, service.ResourceLinkInput(req))
		reply(h, w, r, http.StatusOK, l, err, toResource)
	}
}

func (h *Handler) deleteResource(w http.ResponseWriter, r *http.Request) {
	if gid, id, ok := h.groupAndID(w, r); ok {
		h.deleted(w, r, h.svc.DeleteResourceLink(r.Context(), gid, id))
	}
}
