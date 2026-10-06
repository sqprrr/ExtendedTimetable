package service

import (
	"context"
	"errors"
	"strings"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// SubjectInput is the subject form.
type SubjectInput struct {
	Name      string
	ShortName string
}

func (in SubjectInput) validate() (SubjectInput, error) {
	var err error
	if in.Name, err = text("name", "name", in.Name, true, maxNameLen); err != nil {
		return in, err
	}
	if in.ShortName, err = text("short_name", "short name", in.ShortName, false, maxShortNameLen); err != nil {
		return in, err
	}
	return in, nil
}

var errSubjectExists = &InputError{Field: "name", Msg: "the group already has a subject with this name"}

// checkSubjectName rejects a name another subject of the group already has,
// ignoring case. The UNIQUE index only folds ASCII, which misses Cyrillic.
func checkSubjectName(ctx context.Context, q *store.Queries, groupID, selfID int64, name string) error {
	subjects, err := q.ListSubjects(ctx, groupID)
	if err != nil {
		return err
	}
	for _, other := range subjects {
		if other.ID != selfID && strings.EqualFold(other.Name, name) {
			return errSubjectExists
		}
	}
	return nil
}

// Subjects lists the group's subjects.
func (s *Service) Subjects(ctx context.Context, groupID int64) ([]*store.Subject, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	return s.store.ListSubjects(ctx, groupID)
}

// Subject returns one of the group's subjects.
func (s *Service) Subject(ctx context.Context, groupID, id int64) (*store.Subject, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	sub, err := s.store.SubjectByID(ctx, groupID, id)
	return sub, notFound(err)
}

// CreateSubject adds a subject to the group.
func (s *Service) CreateSubject(ctx context.Context, groupID int64, in SubjectInput) (*store.Subject, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	sub := &store.Subject{GroupID: groupID, Name: in.Name, ShortName: in.ShortName}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := checkSubjectName(ctx, q, groupID, 0, in.Name); err != nil {
			return err
		}
		return q.CreateSubject(ctx, sub)
	})
	if errors.Is(err, store.ErrConflict) {
		return nil, errSubjectExists
	} else if err != nil {
		return nil, err
	}
	return sub, nil
}

// UpdateSubject renames a subject.
func (s *Service) UpdateSubject(ctx context.Context, groupID, id int64, in SubjectInput) (*store.Subject, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	sub := &store.Subject{ID: id, GroupID: groupID, Name: in.Name, ShortName: in.ShortName}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := checkSubjectName(ctx, q, groupID, id, in.Name); err != nil {
			return err
		}
		return q.UpdateSubject(ctx, sub)
	})
	if errors.Is(err, store.ErrConflict) {
		return nil, errSubjectExists
	} else if err != nil {
		return nil, notFound(err)
	}
	return sub, nil
}

// DeleteSubject removes a subject that nothing refers to any more.
func (s *Service) DeleteSubject(ctx context.Context, groupID, id int64) error {
	if _, err := canManage(ctx, groupID); err != nil {
		return err
	}
	err := s.store.DeleteSubject(ctx, groupID, id)
	if errors.Is(err, store.ErrReferenced) {
		return ErrSubjectInUse
	}
	return notFound(err)
}
