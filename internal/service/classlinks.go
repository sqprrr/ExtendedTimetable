package service

import (
	"context"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// LessonTypes lists the lesson types in display order.
var LessonTypes = []store.LessonType{store.LessonLecture, store.LessonPractice, store.LessonLab}

// ClassLinkInput is the class link form.
type ClassLinkInput struct {
	SubjectID  int64
	LessonType store.LessonType
	URL        string
	Note       string
}

func (in ClassLinkInput) validate() (ClassLinkInput, error) {
	var err error
	valid := false
	for _, t := range LessonTypes {
		valid = valid || in.LessonType == t
	}
	if !valid {
		return in, &InputError{Field: "lesson_type", Msg: "choose lecture, practice or lab"}
	}
	if in.URL, err = link("url", in.URL); err != nil {
		return in, err
	}
	if in.Note, err = text("note", "note", in.Note, false, maxNoteLen); err != nil {
		return in, err
	}
	return in, nil
}

// ClassLinks lists the group's class links by subject and lesson type.
func (s *Service) ClassLinks(ctx context.Context, groupID int64) ([]*store.ClassLink, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	return s.store.ListClassLinks(ctx, groupID)
}

// ClassLink returns one of the group's class links.
func (s *Service) ClassLink(ctx context.Context, groupID, id int64) (*store.ClassLink, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	l, err := s.store.ClassLinkByID(ctx, groupID, id)
	return l, notFound(err)
}

// CreateClassLink adds a meeting link for a subject's lesson type.
func (s *Service) CreateClassLink(ctx context.Context, groupID int64, in ClassLinkInput) (*store.ClassLink, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	l := &store.ClassLink{GroupID: groupID, SubjectID: in.SubjectID, LessonType: in.LessonType, URL: in.URL, Note: in.Note}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := checkSubject(ctx, q, groupID, in.SubjectID); err != nil {
			return err
		}
		if err := q.CreateClassLink(ctx, l); err != nil {
			return err
		}
		var err error
		l, err = q.ClassLinkByID(ctx, groupID, l.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}

// UpdateClassLink changes a class link.
func (s *Service) UpdateClassLink(ctx context.Context, groupID, id int64, in ClassLinkInput) (*store.ClassLink, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	l := &store.ClassLink{ID: id, GroupID: groupID, SubjectID: in.SubjectID, LessonType: in.LessonType, URL: in.URL, Note: in.Note}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := checkSubject(ctx, q, groupID, in.SubjectID); err != nil {
			return err
		}
		if err := q.UpdateClassLink(ctx, l); err != nil {
			return notFound(err)
		}
		var err error
		l, err = q.ClassLinkByID(ctx, groupID, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}

// DeleteClassLink removes a class link.
func (s *Service) DeleteClassLink(ctx context.Context, groupID, id int64) error {
	if _, err := canManage(ctx, groupID); err != nil {
		return err
	}
	return notFound(s.store.DeleteClassLink(ctx, groupID, id))
}
