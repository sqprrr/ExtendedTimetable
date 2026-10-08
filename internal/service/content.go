package service

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
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
	maxPoints       = 10000
)

// ErrSubjectInUse is returned when deleting a subject that still has class
// links, homework or resource links.
var ErrSubjectInUse = errors.New("this subject still has class links, homework or recordings; delete or move them first")

// GroupView is a group together with what the viewer may do in it.
type GroupView struct {
	*store.Group
	CanManage bool
	// CanTrack is true when the viewer has a homework tracker here.
	CanTrack bool
	// Role is the viewer's role, or "" for a superadmin who is not a member.
	Role store.Role
	// Leader is the leader's username, or "" while the group has none.
	Leader string
	// IsSuperadmin lets the members page offer the superadmins' actions.
	IsSuperadmin bool
}

// HomeGroup returns the group the navigation points to on pages outside a
// group (the dashboard, feedback): the viewer's first group, or nil when they
// are in none.
func (s *Service) HomeGroup(ctx context.Context) (*GroupView, error) {
	v, err := requireViewer(ctx)
	if err != nil || len(v.Memberships) == 0 {
		return nil, err
	}
	id := v.Memberships[0].GroupID
	g, err := s.store.GroupByID(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	gv := &GroupView{Group: g, CanManage: v.CanManageGroup(id), CanTrack: v.CanTrackGroup(id), IsSuperadmin: v.IsSuperadmin}
	gv.Role, _ = v.RoleIn(id)
	return gv, nil
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
	gv := &GroupView{Group: g, CanManage: v.CanManageGroup(g.ID), CanTrack: v.CanTrackGroup(g.ID), IsSuperadmin: v.IsSuperadmin}
	gv.Role, _ = v.RoleIn(g.ID)
	if lead, err := s.store.LeaderOf(ctx, g.ID); err == nil {
		gv.Leader = lead.Username
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return gv, nil
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

// checkSubject verifies that subjectID is one of the group's subjects.
func checkSubject(ctx context.Context, q *store.Queries, groupID, subjectID int64) error {
	if _, err := q.SubjectByID(ctx, groupID, subjectID); errors.Is(err, store.ErrNotFound) {
		return inputError("subject_id", "err.choose_subject")
	} else if err != nil {
		return err
	}
	return nil
}

// text trims s and checks its length in characters. label is the message ID
// of the field's name.
func text(field, label, s string, required bool, maxLen int) (string, error) {
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", inputError(field, "err.required", "Field", i18n.M(label))
	}
	if utf8.RuneCountInString(s) > maxLen {
		return "", inputError(field, "err.too_long", "Field", i18n.M(label), "Count", maxLen)
	}
	if strings.ContainsFunc(s, unicode.IsControl) {
		return "", inputError(field, "err.single_line", "Field", i18n.M(label))
	}
	return s, nil
}

// markdown checks a Markdown body. Leading whitespace is kept since it can be
// meaningful (an indented code block).
func markdown(field, label, s string) (string, error) {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), " \t\n")
	if utf8.RuneCountInString(s) > maxMarkdownLen {
		return "", inputError(field, "err.too_long", "Field", i18n.M(label), "Count", maxMarkdownLen)
	}
	return s, nil
}

// link checks that raw is an absolute http(s) URL, which keeps javascript:
// and other surprising schemes out of the pages.
func link(field, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", inputError(field, "err.link_required")
	}
	if len(raw) > maxURLLen {
		return "", inputError(field, "err.too_long", "Field", i18n.M("field.link"), "Count", maxURLLen)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", inputError(field, "err.link_invalid")
	}
	return u.String(), nil
}

// points checks an optional positive score.
func points(field string, p *float64) (*float64, error) {
	if p == nil {
		return nil, nil
	}
	if math.IsNaN(*p) || math.IsInf(*p, 0) || *p <= 0 {
		return nil, inputError(field, "err.max_points_positive")
	}
	if *p > maxPoints {
		return nil, inputError(field, "err.max_points_too_big", "Max", maxPoints)
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
		return "", inputError(field, "err.date_invalid")
	}
	return s, nil
}
