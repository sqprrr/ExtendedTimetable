package service

import (
	"context"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// NoteInput is the note form.
type NoteInput struct {
	Title  string
	Body   string // Markdown
	Pinned bool
}

func (in NoteInput) validate() (NoteInput, error) {
	var err error
	if in.Title, err = text("title", "title", in.Title, true, maxTitleLen); err != nil {
		return in, err
	}
	if in.Body, err = markdown("body", "text", in.Body); err != nil {
		return in, err
	}
	return in, nil
}

// Notes lists the group's notes, pinned first, newest first.
func (s *Service) Notes(ctx context.Context, groupID int64) ([]*store.Note, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	return s.store.ListNotes(ctx, groupID)
}

// Note returns one of the group's notes.
func (s *Service) Note(ctx context.Context, groupID, id int64) (*store.Note, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	n, err := s.store.NoteByID(ctx, groupID, id)
	return n, notFound(err)
}

// CreateNote posts a note.
func (s *Service) CreateNote(ctx context.Context, groupID int64, in NoteInput) (*store.Note, error) {
	v, err := canManage(ctx, groupID)
	if err != nil {
		return nil, err
	}
	in, err = in.validate()
	if err != nil {
		return nil, err
	}
	now := s.now()
	n := &store.Note{
		GroupID: groupID, Title: in.Title, BodyMD: in.Body, Pinned: in.Pinned,
		CreatedBy: &v.UserID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateNote(ctx, n); err != nil {
		return nil, err
	}
	return n, nil
}

// UpdateNote changes a note.
func (s *Service) UpdateNote(ctx context.Context, groupID, id int64, in NoteInput) (*store.Note, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	var n *store.Note
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if n, err = q.NoteByID(ctx, groupID, id); err != nil {
			return notFound(err)
		}
		n.Title, n.BodyMD, n.Pinned, n.UpdatedAt = in.Title, in.Body, in.Pinned, s.now()
		return q.UpdateNote(ctx, n)
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}

// DeleteNote removes a note.
func (s *Service) DeleteNote(ctx context.Context, groupID, id int64) error {
	if _, err := canManage(ctx, groupID); err != nil {
		return err
	}
	return notFound(s.store.DeleteNote(ctx, groupID, id))
}
