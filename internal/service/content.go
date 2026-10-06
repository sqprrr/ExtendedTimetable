package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Group content (subjects, class links, homework, notes, recordings and
// solutions) is readable by the group's members and superadmins, and writable
// by the group's leaders and superadmins.

const (
	maxNameLen      = 100
	maxShortNameLen = 20
	maxTitleLen     = 200
	maxNoteLen      = 200
	maxURLLen       = 2000
	maxMarkdownLen  = 20000
	maxHomeworkURLs = 20
)

// ErrSubjectInUse is returned when deleting a subject that still has class
// links, homework or resource links.
var ErrSubjectInUse = errors.New("this subject still has class links, homework or recordings; delete or move them first")

// GroupView is a group together with what the viewer may do in it.
type GroupView struct {
	*store.Group
	CanManage bool
}

// Group returns the group with the given code if the viewer may see it.
func (s *Service) Group(ctx context.Context, code string) (*GroupView, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.store.GroupByCode(ctx, strings.TrimSpace(code))
	if err != nil {
		return nil, notFound(err)
	}
	if !v.CanViewGroup(g.ID) {
		return nil, ErrForbidden
	}
	return &GroupView{Group: g, CanManage: v.CanManageGroup(g.ID)}, nil
}

// canView checks that the viewer may read the group's content.
func canView(ctx context.Context, groupID int64) (*Viewer, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	if !v.CanViewGroup(groupID) {
		return nil, ErrForbidden
	}
	return v, nil
}

// canManage checks that the viewer may change the group's content.
func canManage(ctx context.Context, groupID int64) (*Viewer, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	if !v.CanManageGroup(groupID) {
		return nil, ErrForbidden
	}
	return v, nil
}

// notFound maps store.ErrNotFound to ErrNotFound and passes other errors on.
func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// subjectNames maps the group's subject ids to their display names.
func (s *Service) subjectNames(ctx context.Context, groupID int64) (map[int64]string, error) {
	subjects, err := s.store.ListSubjects(ctx, groupID)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(subjects))
	for _, sub := range subjects {
		names[sub.ID] = sub.Name
	}
	return names, nil
}

// checkSubject verifies that subjectID is one of the group's subjects.
func checkSubject(ctx context.Context, q *store.Queries, groupID, subjectID int64) error {
	if _, err := q.SubjectByID(ctx, groupID, subjectID); errors.Is(err, store.ErrNotFound) {
		return &InputError{Field: "subject_id", Msg: "choose a subject from the list"}
	} else if err != nil {
		return err
	}
	return nil
}

// text trims s and checks its length in characters.
func text(field, label, s string, required bool, maxLen int) (string, error) {
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", &InputError{Field: field, Msg: label + " is required"}
	}
	if utf8.RuneCountInString(s) > maxLen {
		return "", &InputError{Field: field, Msg: fmt.Sprintf("%s must be at most %d characters", label, maxLen)}
	}
	return s, nil
}

// markdown checks a Markdown body. Leading whitespace is kept since it can be
// meaningful (an indented code block).
func markdown(field, label, s string) (string, error) {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), " \t\n")
	if utf8.RuneCountInString(s) > maxMarkdownLen {
		return "", &InputError{Field: field, Msg: fmt.Sprintf("%s must be at most %d characters", label, maxMarkdownLen)}
	}
	return s, nil
}

// link checks that raw is an absolute http(s) URL, which keeps javascript:
// and other surprising schemes out of the pages.
func link(field, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", &InputError{Field: field, Msg: "link is required"}
	}
	if len(raw) > maxURLLen {
		return "", &InputError{Field: field, Msg: fmt.Sprintf("link must be at most %d characters", maxURLLen)}
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", &InputError{Field: field, Msg: "link must be a full http:// or https:// address"}
	}
	return u.String(), nil
}

// points checks an optional positive score.
func points(field string, p *float64) (*float64, error) {
	if p == nil {
		return nil, nil
	}
	if math.IsNaN(*p) || math.IsInf(*p, 0) || *p <= 0 || *p > 10000 {
		return nil, &InputError{Field: field, Msg: "max points must be a positive number"}
	}
	v := *p
	return &v, nil
}

// date checks an optional "YYYY-MM-DD" date.
func date(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return "", &InputError{Field: field, Msg: "date must look like 2026-09-01"}
	}
	return s, nil
}
