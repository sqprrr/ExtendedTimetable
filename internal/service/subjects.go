package service

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// SubjectInput is the subject form.
type SubjectInput struct {
	Name      string
	ShortName string
	// Hue is one of Hues, or "" for the default.
	Hue string
	// Lecturer, Instructor (practice classes and labs) and DLURL (the
	// distance-learning page) are optional.
	Lecturer   string
	Instructor string
	DLURL      string
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
	if in.Lecturer, err = text("lecturer", "field.lecturer", in.Lecturer, false, maxNameLen); err != nil {
		return in, err
	}
	if in.Instructor, err = text("instructor", "field.instructor", in.Instructor, false, maxNameLen); err != nil {
		return in, err
	}
	if in.DLURL = strings.TrimSpace(in.DLURL); in.DLURL != "" {
		if in.DLURL, err = link("dl_url", in.DLURL); err != nil {
			return in, err
		}
	}
	return in, nil
}

// subject builds the stored subject from validated input.
func (in SubjectInput) subject(groupID, id int64) *store.Subject {
	return &store.Subject{
		ID: id, GroupID: groupID, Name: in.Name, ShortName: in.ShortName, Hue: in.Hue,
		Lecturer: in.Lecturer, Instructor: in.Instructor, DLURL: in.DLURL,
	}
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
	sub := in.subject(groupID, 0)
	if err := s.store.CreateSubject(ctx, sub); errors.Is(err, store.ErrConflict) {
		return nil, errSubjectExists
	} else if err != nil {
		return nil, err
	}
	return sub, nil
}

// UpdateSubject changes a subject's name, colour, teachers or DL page.
func (s *Service) UpdateSubject(ctx context.Context, groupID, id int64, in SubjectInput) (*store.Subject, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	sub := in.subject(groupID, id)
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

// SubjectPage is everything about one subject: its class links, homework
// and recordings and solutions.
type SubjectPage struct {
	Subject *store.Subject
	// ClassLinks are the subject's meeting links, lectures first.
	ClassLinks []*store.ClassLink
	// Homework is the subject's homework with the viewer's progress, in the
	// order of the homework list.
	Homework []*Homework
	// Resources are the subject's recordings and solutions, newest first.
	Resources []*store.ResourceLink
}

// SubjectPage returns the subject page. Members of the group and
// superadmins only.
func (s *Service) SubjectPage(ctx context.Context, groupID, id int64) (*SubjectPage, error) {
	sub, err := s.Subject(ctx, groupID, id)
	if err != nil {
		return nil, err
	}
	p := &SubjectPage{Subject: sub}
	links, err := s.store.ListClassLinks(ctx, groupID)
	if err != nil {
		return nil, err
	}
	for _, t := range LessonTypes {
		for _, l := range links {
			if l.SubjectID == id && l.LessonType == t {
				p.ClassLinks = append(p.ClassLinks, l)
			}
		}
	}
	if p.Homework, err = s.HomeworkList(ctx, groupID, HomeworkFilter{SubjectID: id}); err != nil {
		return nil, err
	}
	resources, err := s.store.ListResourceLinks(ctx, groupID)
	if err != nil {
		return nil, err
	}
	for _, r := range resources {
		if r.SubjectID == id {
			p.Resources = append(p.Resources, r)
		}
	}
	return p, nil
}
