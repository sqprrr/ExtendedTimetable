package service

import (
	"context"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// ResourceLink is a recording or solution link with its subject's name.
type ResourceLink struct {
	store.ResourceLink
	SubjectName string
}

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
func (s *Service) ResourceLinks(ctx context.Context, groupID int64) ([]*ResourceLink, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	links, err := s.store.ListResourceLinks(ctx, groupID)
	if err != nil {
		return nil, err
	}
	names, err := s.subjectNames(ctx, groupID)
	if err != nil {
		return nil, err
	}
	out := make([]*ResourceLink, 0, len(links))
	for _, l := range links {
		out = append(out, &ResourceLink{ResourceLink: *l, SubjectName: names[l.SubjectID]})
	}
	return out, nil
}

// ResourceLink returns one of the group's recordings or solutions.
func (s *Service) ResourceLink(ctx context.Context, groupID, id int64) (*ResourceLink, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	l, err := s.store.ResourceLinkByID(ctx, groupID, id)
	if err != nil {
		return nil, notFound(err)
	}
	sub, err := s.store.SubjectByID(ctx, groupID, l.SubjectID)
	if err != nil {
		return nil, err
	}
	return &ResourceLink{ResourceLink: *l, SubjectName: sub.Name}, nil
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
		return q.CreateResourceLink(ctx, l)
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
		if l, err = q.ResourceLinkByID(ctx, groupID, id); err != nil {
			return notFound(err)
		}
		if err := checkSubject(ctx, q, groupID, in.SubjectID); err != nil {
			return err
		}
		l.SubjectID, l.Kind, l.Title, l.URL, l.Date = in.SubjectID, in.Kind, in.Title, in.URL, in.Date
		return q.UpdateResourceLink(ctx, l)
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
