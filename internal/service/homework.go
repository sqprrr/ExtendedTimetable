package service

import (
	"context"
	"errors"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Homework is an assignment as shown to the viewer, with the viewer's own
// progress on it.
type Homework struct {
	store.Homework
	// Links is filled for a single assignment, not for lists.
	Links []*store.HomeworkLink
	// Tracked is false when the viewer has no tracker in the group (a
	// superadmin who is not a member); Status and Grade are then unset.
	Tracked bool
	Status  store.ProgressStatus
	Grade   *float64
	// Overdue is true once the deadline has passed and the viewer has not
	// marked the assignment done.
	Overdue bool
	// GradeOverMax is true when the viewer's grade is above the max points,
	// which happens if the leader lowers them after the grade was saved.
	GradeOverMax bool
}

// LinkInput is one related link of an assignment. Title is optional.
type LinkInput struct {
	Title string
	URL   string
}

// HomeworkInput is the homework form.
type HomeworkInput struct {
	SubjectID   int64
	Title       string
	Description string // Markdown
	// DueAt is nil for no deadline.
	DueAt     *time.Time
	MaxPoints *float64
	Links     []LinkInput
}

func (in HomeworkInput) validate() (HomeworkInput, []*store.HomeworkLink, error) {
	var err error
	if in.Title, err = text("title", "field.title", in.Title, true, maxTitleLen); err != nil {
		return in, nil, err
	}
	if in.Description, err = markdown("description", "field.description_md", in.Description); err != nil {
		return in, nil, err
	}
	if in.MaxPoints, err = points("max_points", in.MaxPoints); err != nil {
		return in, nil, err
	}
	if len(in.Links) > maxHomeworkURLs {
		return in, nil, inputError("links", "err.too_many_links", "Count", maxHomeworkURLs)
	}
	links := make([]*store.HomeworkLink, 0, len(in.Links))
	for _, l := range in.Links {
		title, err := text("links", "field.link_title", l.Title, false, maxTitleLen)
		if err != nil {
			return in, nil, err
		}
		u, err := link("links", l.URL)
		if err != nil {
			return in, nil, err
		}
		links = append(links, &store.HomeworkLink{Title: title, URL: u})
	}
	return in, links, nil
}

// HomeworkFilter narrows a homework list. Zero fields match everything.
type HomeworkFilter struct {
	SubjectID int64
	// Status is the viewer's own status; it is ignored for a viewer without
	// a tracker in the group.
	Status store.ProgressStatus
}

// match reports whether the viewer's assignment passes the filter.
func (f HomeworkFilter) match(h *Homework) bool {
	if f.SubjectID != 0 && h.SubjectID != f.SubjectID {
		return false
	}
	return f.Status == "" || !h.Tracked || h.Status == f.Status
}

// HomeworkList lists the group's assignments that pass f by due date, those
// without a deadline last. An unknown status in f is an input error.
func (s *Service) HomeworkList(ctx context.Context, groupID int64, f HomeworkFilter) ([]*Homework, error) {
	v, err := canView(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if f.Status != "" && !validStatus(f.Status) {
		return nil, inputError("status", "err.status")
	}
	hws, err := s.store.ListHomework(ctx, groupID)
	if err != nil {
		return nil, err
	}
	views, err := s.homeworkViews(ctx, v, groupID, hws)
	if err != nil {
		return nil, err
	}
	out := views[:0]
	for _, h := range views {
		if f.match(h) {
			out = append(out, h)
		}
	}
	return out, nil
}

// homeworkViews adds the viewer's progress to a list of the group's assignments.
func (s *Service) homeworkViews(ctx context.Context, v *Viewer, groupID int64, hws []*store.Homework) ([]*Homework, error) {
	tracked := v.CanTrackGroup(groupID)
	var progress map[int64]*store.Progress
	if tracked {
		var err error
		if progress, err = s.myProgress(ctx, v, groupID); err != nil {
			return nil, err
		}
	}
	now := s.now()
	out := make([]*Homework, 0, len(hws))
	for _, h := range hws {
		out = append(out, homeworkView(h, tracked, progress[h.ID], now))
	}
	return out, nil
}

// myProgress returns the viewer's progress in a group by homework id.
func (s *Service) myProgress(ctx context.Context, v *Viewer, groupID int64) (map[int64]*store.Progress, error) {
	ps, err := s.store.ListProgress(ctx, v.UserID, groupID)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]*store.Progress, len(ps))
	for _, p := range ps {
		m[p.HomeworkID] = p
	}
	return m, nil
}

// homeworkView builds what the viewer sees; p is nil when they have not
// touched the assignment yet.
func homeworkView(h *store.Homework, tracked bool, p *store.Progress, now time.Time) *Homework {
	out := &Homework{Homework: *h, Tracked: tracked}
	if tracked {
		out.Status = store.StatusNotStarted
		if p != nil {
			out.Status, out.Grade = p.Status, p.Grade
		}
	}
	out.Overdue = h.DueAt != nil && h.DueAt.Before(now) && out.Status != store.StatusDone
	out.GradeOverMax = out.Grade != nil && h.MaxPoints != nil && *out.Grade > *h.MaxPoints
	return out
}

// Homework returns one of the group's assignments with its links.
func (s *Service) Homework(ctx context.Context, groupID, id int64) (*Homework, error) {
	v, err := canView(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return s.homeworkWithLinks(ctx, s.store.Queries, v, groupID, id)
}

func (s *Service) homeworkWithLinks(ctx context.Context, q *store.Queries, v *Viewer, groupID, id int64) (*Homework, error) {
	h, err := q.HomeworkByID(ctx, groupID, id)
	if err != nil {
		return nil, notFound(err)
	}
	tracked := v.CanTrackGroup(groupID)
	var p *store.Progress
	if tracked {
		if p, err = q.ProgressFor(ctx, v.UserID, id); err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	out := homeworkView(h, tracked, p, s.now())
	if out.Links, err = q.HomeworkLinks(ctx, h.ID); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateHomework posts an assignment.
func (s *Service) CreateHomework(ctx context.Context, groupID int64, in HomeworkInput) (*Homework, error) {
	v, err := canManage(ctx, groupID)
	if err != nil {
		return nil, err
	}
	in, links, err := in.validate()
	if err != nil {
		return nil, err
	}
	now := s.now()
	h := &store.Homework{
		GroupID: groupID, SubjectID: in.SubjectID, Title: in.Title, DescriptionMD: in.Description,
		DueAt: in.DueAt, MaxPoints: in.MaxPoints, CreatedBy: &v.UserID, CreatedAt: now, UpdatedAt: now,
	}
	var out *Homework
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := checkSubject(ctx, q, groupID, in.SubjectID); err != nil {
			return err
		}
		if err := q.CreateHomework(ctx, h); err != nil {
			return err
		}
		if err := q.ReplaceHomeworkLinks(ctx, h.ID, links); err != nil {
			return err
		}
		var err error
		out, err = s.homeworkWithLinks(ctx, q, v, groupID, h.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateHomework changes an assignment and replaces its links.
func (s *Service) UpdateHomework(ctx context.Context, groupID, id int64, in HomeworkInput) (*Homework, error) {
	v, err := canManage(ctx, groupID)
	if err != nil {
		return nil, err
	}
	in, links, err := in.validate()
	if err != nil {
		return nil, err
	}
	var out *Homework
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		h, err := q.HomeworkByID(ctx, groupID, id)
		if err != nil {
			return notFound(err)
		}
		if err := checkSubject(ctx, q, groupID, in.SubjectID); err != nil {
			return err
		}
		h.SubjectID, h.Title, h.DescriptionMD = in.SubjectID, in.Title, in.Description
		h.DueAt, h.MaxPoints, h.UpdatedAt = in.DueAt, in.MaxPoints, s.now()
		if err := q.UpdateHomework(ctx, h); err != nil {
			return err
		}
		if err := q.ReplaceHomeworkLinks(ctx, h.ID, links); err != nil {
			return err
		}
		out, err = s.homeworkWithLinks(ctx, q, v, groupID, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteHomework removes an assignment and its links.
func (s *Service) DeleteHomework(ctx context.Context, groupID, id int64) error {
	if _, err := canManage(ctx, groupID); err != nil {
		return err
	}
	return notFound(s.store.DeleteHomework(ctx, groupID, id))
}
