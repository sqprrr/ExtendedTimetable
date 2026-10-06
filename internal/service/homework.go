package service

import (
	"context"
	"fmt"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Homework is an assignment as shown to the viewer.
type Homework struct {
	store.Homework
	// Links is filled for a single assignment, not for lists.
	Links []*store.HomeworkLink
	// Overdue is true once the deadline has passed.
	Overdue bool
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
	if in.Title, err = text("title", "title", in.Title, true, maxTitleLen); err != nil {
		return in, nil, err
	}
	if in.Description, err = markdown("description", "description", in.Description); err != nil {
		return in, nil, err
	}
	if in.MaxPoints, err = points("max_points", in.MaxPoints); err != nil {
		return in, nil, err
	}
	if len(in.Links) > maxHomeworkURLs {
		return in, nil, &InputError{Field: "links", Msg: fmt.Sprintf("at most %d links", maxHomeworkURLs)}
	}
	links := make([]*store.HomeworkLink, 0, len(in.Links))
	for _, l := range in.Links {
		title, err := text("links", "link title", l.Title, false, maxTitleLen)
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

// HomeworkList lists the group's assignments by due date, those without a
// deadline last.
func (s *Service) HomeworkList(ctx context.Context, groupID int64) ([]*Homework, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	hws, err := s.store.ListHomework(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return s.homeworkViews(hws), nil
}

func (s *Service) homeworkViews(hws []*store.Homework) []*Homework {
	now := s.now()
	out := make([]*Homework, 0, len(hws))
	for _, h := range hws {
		out = append(out, homeworkView(h, now))
	}
	return out
}

func homeworkView(h *store.Homework, now time.Time) *Homework {
	return &Homework{Homework: *h, Overdue: h.DueAt != nil && h.DueAt.Before(now)}
}

// Homework returns one of the group's assignments with its links.
func (s *Service) Homework(ctx context.Context, groupID, id int64) (*Homework, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	return s.homeworkWithLinks(ctx, s.store.Queries, groupID, id)
}

func (s *Service) homeworkWithLinks(ctx context.Context, q *store.Queries, groupID, id int64) (*Homework, error) {
	h, err := q.HomeworkByID(ctx, groupID, id)
	if err != nil {
		return nil, notFound(err)
	}
	out := homeworkView(h, s.now())
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
		out, err = s.homeworkWithLinks(ctx, q, groupID, h.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateHomework changes an assignment and replaces its links.
func (s *Service) UpdateHomework(ctx context.Context, groupID, id int64, in HomeworkInput) (*Homework, error) {
	if _, err := canManage(ctx, groupID); err != nil {
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
		out, err = s.homeworkWithLinks(ctx, q, groupID, id)
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
