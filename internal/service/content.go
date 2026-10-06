package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

const (
	maxTitleLen  = 200
	maxShortLen  = 20
	maxNameLen   = 100
	maxBodyLen   = 50_000
	maxURLLen    = 2_000
	maxNoteLen   = 500
	maxMaxPoints = 1_000
	dateLayout   = "2006-01-02"
)

var (
	lessonTypes   = map[string]bool{"lecture": true, "practice": true, "lab": true}
	resourceKinds = map[string]bool{"recording": true, "solution": true}
)

// HomeworkItem is an assignment with its attached links.
type HomeworkItem struct {
	store.Homework
	Links   []store.HomeworkLink
	Overdue bool
}

// GroupPage is everything a group's page shows.
type GroupPage struct {
	Group      *store.Group
	CanManage  bool
	Subjects   []store.Subject
	ClassLinks []store.ClassLink
	Homework   []HomeworkItem
	Notes      []store.Note
	Resources  []store.ResourceLink
}

// SubjectInput is a subject as entered by a leader.
type SubjectInput struct {
	Name      string
	ShortName string
}

// ClassLinkInput is a meeting link as entered by a leader.
type ClassLinkInput struct {
	SubjectID  int64
	LessonType string
	URL        string
	Note       string
}

// HomeworkInput is an assignment as entered by a leader.
type HomeworkInput struct {
	SubjectID     int64
	Title         string
	DescriptionMD string
	DueAt         time.Time
	MaxPoints     *float64
}

// HomeworkLinkInput is a link attached to an assignment.
type HomeworkLinkInput struct {
	Title string
	URL   string
}

// NoteInput is a note as entered by a leader.
type NoteInput struct {
	Title  string
	BodyMD string
	Pinned bool
}

// ResourceInput is a recording or solution link as entered by a leader.
type ResourceInput struct {
	SubjectID int64
	Kind      string
	Title     string
	URL       string
	// Date is the day the recording or solution belongs to; zero means unknown.
	Date time.Time
}

// groupForView loads a group the viewer may see: its members and superadmins.
func (s *Service) groupForView(ctx context.Context, code string) (*store.Group, *Viewer, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, nil, err
	}
	g, err := s.store.GroupByCode(ctx, strings.TrimSpace(code))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	if _, member := v.RoleIn(g.ID); !member && !v.IsSuperadmin {
		return nil, nil, ErrForbidden
	}
	return g, v, nil
}

// groupForManage loads a group the viewer may edit: its leaders and superadmins.
func (s *Service) groupForManage(ctx context.Context, code string) (*store.Group, *Viewer, error) {
	g, v, err := s.groupForView(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	if !v.CanManageGroup(g.ID) {
		return nil, nil, ErrForbidden
	}
	return g, v, nil
}

// GroupPage returns a group's content for a member or superadmin.
func (s *Service) GroupPage(ctx context.Context, code string) (*GroupPage, error) {
	g, v, err := s.groupForView(ctx, code)
	if err != nil {
		return nil, err
	}
	subjects, err := s.store.SubjectsByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	classLinks, err := s.store.ClassLinksByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	hw, err := s.store.HomeworkByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	hwLinks, err := s.store.HomeworkLinksByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	notes, err := s.store.NotesByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	resources, err := s.store.ResourcesByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}

	linksByHW := map[int64][]store.HomeworkLink{}
	for _, l := range hwLinks {
		linksByHW[l.HomeworkID] = append(linksByHW[l.HomeworkID], l)
	}
	now := s.now()
	items := make([]HomeworkItem, 0, len(hw))
	for _, h := range hw {
		items = append(items, HomeworkItem{Homework: h, Links: linksByHW[h.ID], Overdue: h.DueAt.Before(now)})
	}
	return &GroupPage{
		Group:      g,
		CanManage:  v.CanManageGroup(g.ID),
		Subjects:   subjects,
		ClassLinks: classLinks,
		Homework:   items,
		Notes:      notes,
		Resources:  resources,
	}, nil
}

// SaveSubject creates a subject when id is zero, otherwise updates subject id.
func (s *Service) SaveSubject(ctx context.Context, groupCode string, id int64, in SubjectInput) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	name, err := cleanText("name", in.Name, maxNameLen, true)
	if err != nil {
		return err
	}
	short, err := cleanText("short_name", in.ShortName, maxShortLen, false)
	if err != nil {
		return err
	}
	sub := &store.Subject{ID: id, GroupID: g.ID, Name: name, ShortName: short}
	return mapStoreErr(s.store.SaveSubject(ctx, sub), func() error {
		return &InputError{Field: "name", Msg: "this group already has a subject with that name"}
	})
}

// DeleteSubject removes a subject along with its homework, links and resources.
func (s *Service) DeleteSubject(ctx context.Context, groupCode string, id int64) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	return mapStoreErr(s.store.DeleteSubject(ctx, g.ID, id), nil)
}

// SaveClassLink creates a class link when id is zero, otherwise updates link id.
func (s *Service) SaveClassLink(ctx context.Context, groupCode string, id int64, in ClassLinkInput) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	if !lessonTypes[in.LessonType] {
		return &InputError{Field: "lesson_type", Msg: "choose lecture, practice or lab"}
	}
	if err := s.checkSubject(ctx, g.ID, in.SubjectID); err != nil {
		return err
	}
	link, err := cleanURL(in.URL)
	if err != nil {
		return err
	}
	note, err := cleanText("note", in.Note, maxNoteLen, false)
	if err != nil {
		return err
	}
	cl := &store.ClassLink{
		ID: id, GroupID: g.ID, SubjectID: in.SubjectID, LessonType: in.LessonType, URL: link, Note: note,
	}
	return mapStoreErr(s.store.SaveClassLink(ctx, cl), nil)
}

// DeleteClassLink removes a class link.
func (s *Service) DeleteClassLink(ctx context.Context, groupCode string, id int64) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	return mapStoreErr(s.store.DeleteClassLink(ctx, g.ID, id), nil)
}

// SaveHomework creates an assignment when id is zero, otherwise updates assignment id.
func (s *Service) SaveHomework(ctx context.Context, groupCode string, id int64, in HomeworkInput) error {
	g, v, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	if err := s.checkSubject(ctx, g.ID, in.SubjectID); err != nil {
		return err
	}
	title, err := cleanText("title", in.Title, maxTitleLen, true)
	if err != nil {
		return err
	}
	body, err := cleanText("description", in.DescriptionMD, maxBodyLen, false)
	if err != nil {
		return err
	}
	if in.DueAt.IsZero() {
		return &InputError{Field: "due", Msg: "set a due date"}
	}
	if in.MaxPoints != nil && (*in.MaxPoints <= 0 || *in.MaxPoints > maxMaxPoints) {
		return &InputError{Field: "max_points", Msg: "max points must be between 0 and 1000"}
	}
	creator := v.UserID
	hw := &store.Homework{
		ID: id, GroupID: g.ID, SubjectID: in.SubjectID, Title: title, DescriptionMD: body,
		DueAt: in.DueAt, MaxPoints: in.MaxPoints, CreatedBy: &creator,
	}
	return mapStoreErr(s.store.SaveHomework(ctx, hw, s.now()), nil)
}

// DeleteHomework removes an assignment with its links.
func (s *Service) DeleteHomework(ctx context.Context, groupCode string, id int64) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	return mapStoreErr(s.store.DeleteHomework(ctx, g.ID, id), nil)
}

// AddHomeworkLink attaches a link to assignment homeworkID.
func (s *Service) AddHomeworkLink(ctx context.Context, groupCode string, homeworkID int64, in HomeworkLinkInput) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	title, err := cleanText("title", in.Title, maxTitleLen, true)
	if err != nil {
		return err
	}
	link, err := cleanURL(in.URL)
	if err != nil {
		return err
	}
	hl := &store.HomeworkLink{HomeworkID: homeworkID, Title: title, URL: link}
	return mapStoreErr(s.store.AddHomeworkLink(ctx, g.ID, hl), nil)
}

// DeleteHomeworkLink removes a link from an assignment.
func (s *Service) DeleteHomeworkLink(ctx context.Context, groupCode string, id int64) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	return mapStoreErr(s.store.DeleteHomeworkLink(ctx, g.ID, id), nil)
}

// SaveNote creates a note when id is zero, otherwise updates note id.
func (s *Service) SaveNote(ctx context.Context, groupCode string, id int64, in NoteInput) error {
	g, v, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	title, err := cleanText("title", in.Title, maxTitleLen, true)
	if err != nil {
		return err
	}
	body, err := cleanText("body", in.BodyMD, maxBodyLen, false)
	if err != nil {
		return err
	}
	creator := v.UserID
	n := &store.Note{ID: id, GroupID: g.ID, Title: title, BodyMD: body, Pinned: in.Pinned, CreatedBy: &creator}
	return mapStoreErr(s.store.SaveNote(ctx, n, s.now()), nil)
}

// DeleteNote removes a note.
func (s *Service) DeleteNote(ctx context.Context, groupCode string, id int64) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	return mapStoreErr(s.store.DeleteNote(ctx, g.ID, id), nil)
}

// SaveResource creates a recording or solution link when id is zero, otherwise updates it.
func (s *Service) SaveResource(ctx context.Context, groupCode string, id int64, in ResourceInput) error {
	g, v, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	if !resourceKinds[in.Kind] {
		return &InputError{Field: "kind", Msg: "choose recording or solution"}
	}
	if err := s.checkSubject(ctx, g.ID, in.SubjectID); err != nil {
		return err
	}
	title, err := cleanText("title", in.Title, maxTitleLen, true)
	if err != nil {
		return err
	}
	link, err := cleanURL(in.URL)
	if err != nil {
		return err
	}
	r := &store.ResourceLink{
		ID: id, GroupID: g.ID, SubjectID: in.SubjectID, Kind: in.Kind, Title: title, URL: link,
		CreatedBy: &v.UserID, CreatedAt: s.now(),
	}
	if !in.Date.IsZero() {
		r.Date = in.Date.Format(dateLayout)
	}
	return mapStoreErr(s.store.SaveResource(ctx, r), nil)
}

// DeleteResource removes a recording or solution link.
func (s *Service) DeleteResource(ctx context.Context, groupCode string, id int64) error {
	g, _, err := s.groupForManage(ctx, groupCode)
	if err != nil {
		return err
	}
	return mapStoreErr(s.store.DeleteResource(ctx, g.ID, id), nil)
}

// checkSubject ensures subjectID exists in the group.
func (s *Service) checkSubject(ctx context.Context, groupID, subjectID int64) error {
	_, err := s.store.SubjectByID(ctx, groupID, subjectID)
	if errors.Is(err, store.ErrNotFound) {
		return &InputError{Field: "subject", Msg: "choose a subject from the list"}
	}
	return err
}

// mapStoreErr converts store errors to service errors. conflict, when non-nil,
// builds the error for a UNIQUE violation.
func mapStoreErr(err error, conflict func() error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, store.ErrConflict) && conflict != nil:
		return conflict()
	}
	return err
}

// cleanText trims s and checks its length in runes.
func cleanText(field, s string, maxLen int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", &InputError{Field: field, Msg: "this field is required"}
	}
	if utf8.RuneCountInString(s) > maxLen {
		return "", &InputError{Field: field, Msg: "this field is too long"}
	}
	return s, nil
}

// cleanURL accepts only absolute http(s) links.
func cleanURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLLen {
		return "", &InputError{Field: "url", Msg: "enter a link starting with https://"}
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", &InputError{Field: "url", Msg: "enter a link starting with https://"}
	}
	return raw, nil
}
