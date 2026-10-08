package service

import (
	"context"
	"errors"
	"slices"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// SubjectInput is the subject form.
type SubjectInput struct {
	Name      string
	ShortName string
	// Hue is one of Hues, or "" for the default.
	Hue string
}

// Hues are the subject colours, in the order the defaults go round.
var Hues = []string{"blue", "teal", "green", "amber", "orange", "rose", "violet", "slate"}

// Hue is the colour a subject is shown in: the one stored on it, else one
// picked from its id, so neighbouring subjects differ.
func Hue(subjectID int64, stored string) string {
	if slices.Contains(Hues, stored) {
		return stored
	}
	return Hues[subjectID%int64(len(Hues))]
}

func (in SubjectInput) validate() (SubjectInput, error) {
	var err error
	if in.Name, err = text("name", "field.name", in.Name, true, maxNameLen); err != nil {
		return in, err
	}
	if in.ShortName, err = text("short_name", "field.short_name", in.ShortName, false, maxShortNameLen); err != nil {
		return in, err
	}
	if in.Hue != "" && !slices.Contains(Hues, in.Hue) {
		return in, inputError("hue", "err.hue")
	}
	return in, nil
}

var errSubjectExists = inputError("name", "err.subject_exists")

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
	sub := &store.Subject{GroupID: groupID, Name: in.Name, ShortName: in.ShortName, Hue: in.Hue}
	if err := s.store.CreateSubject(ctx, sub); errors.Is(err, store.ErrConflict) {
		return nil, errSubjectExists
	} else if err != nil {
		return nil, err
	}
	return sub, nil
}

// UpdateSubject renames a subject or changes its colour.
func (s *Service) UpdateSubject(ctx context.Context, groupID, id int64, in SubjectInput) (*store.Subject, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	sub := &store.Subject{ID: id, GroupID: groupID, Name: in.Name, ShortName: in.ShortName, Hue: in.Hue}
	if err := s.store.UpdateSubject(ctx, sub); errors.Is(err, store.ErrConflict) {
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
