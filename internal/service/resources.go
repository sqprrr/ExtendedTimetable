package service

import (
	"context"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// ResourceLinkInput is the recording / solution form.
type ResourceLinkInput struct {
	SubjectID int64
	Kind      store.ResourceKind
	Title     string
	URL       string
	Date      string // "YYYY-MM-DD" or empty
}

func (in ResourceLinkInput) validate() (ResourceLinkInput, error) {
	var err error
	if in.Kind != store.ResourceRecording && in.Kind != store.ResourceSolution {
		return in, &InputError{Field: "kind", Msg: "choose recording or solution"}
	}
	if in.Title, err = text("title", "title", in.Title, true, maxTitleLen); err != nil {
		return in, err
	}
	if in.URL, err = link("url", in.URL); err != nil {
		return in, err
	}
	if in.Date, err = date("date", in.Date); err != nil {
		return in, err
	}
	return in, nil
}

// ResourceLinks lists the group's recordings and solutions, newest first.
func (s *Service) ResourceLinks(ctx context.Context, groupID int64) ([]*store.ResourceLink, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	return s.store.ListResourceLinks(ctx, groupID)
}

// ResourceLink returns one of the group's recordings or solutions.
func (s *Service) ResourceLink(ctx context.Context, groupID, id int64) (*store.ResourceLink, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	l, err := s.store.ResourceLinkByID(ctx, groupID, id)
	return l, notFound(err)
}

// CreateResourceLink adds a recording or solution link.
func (s *Service) CreateResourceLink(ctx context.Context, groupID int64, in ResourceLinkInput) (*store.ResourceLink, error) {
	v, err := canManage(ctx, groupID)
	if err != nil {
		return nil, err
	}
	in, err = in.validate()
	if err != nil {
		return nil, err
	}
	l := &store.ResourceLink{
		GroupID: groupID, SubjectID: in.SubjectID, Kind: in.Kind, Title: in.Title, URL: in.URL, Date: in.Date,
		CreatedBy: &v.UserID, CreatedAt: s.now(),
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := checkSubject(ctx, q, groupID, in.SubjectID); err != nil {
			return err
		}
		if err := q.CreateResourceLink(ctx, l); err != nil {
			return err
		}
		var err error
		l, err = q.ResourceLinkByID(ctx, groupID, l.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}

// UpdateResourceLink changes a recording or solution link.
func (s *Service) UpdateResourceLink(ctx context.Context, groupID, id int64, in ResourceLinkInput) (*store.ResourceLink, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	var l *store.ResourceLink
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		old, err := q.ResourceLinkByID(ctx, groupID, id)
		if err != nil {
			return notFound(err)
		}
		if err := checkSubject(ctx, q, groupID, in.SubjectID); err != nil {
			return err
		}
		old.SubjectID, old.Kind, old.Title, old.URL, old.Date = in.SubjectID, in.Kind, in.Title, in.URL, in.Date
		if err := q.UpdateResourceLink(ctx, old); err != nil {
			return err
		}
		l, err = q.ResourceLinkByID(ctx, groupID, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}

// DeleteResourceLink removes a recording or solution link.
func (s *Service) DeleteResourceLink(ctx context.Context, groupID, id int64) error {
	if _, err := canManage(ctx, groupID); err != nil {
		return err
	}
	return notFound(s.store.DeleteResourceLink(ctx, groupID, id))
}
