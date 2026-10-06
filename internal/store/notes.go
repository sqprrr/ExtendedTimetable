package store

import (
	"context"
	"database/sql"
	"time"
)

// Note is a row of the notes table: an announcement from the leader.
type Note struct {
	ID        int64
	GroupID   int64
	Title     string
	BodyMD    string
	Pinned    bool
	CreatedBy *int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

const noteColumns = `id, group_id, title, body_md, pinned, created_by, created_at, updated_at`

func scanNote(row interface{ Scan(...any) error }) (*Note, error) {
	var n Note
	var createdBy sql.NullInt64
	var created, updated int64
	if err := row.Scan(&n.ID, &n.GroupID, &n.Title, &n.BodyMD, &n.Pinned, &createdBy, &created, &updated); err != nil {
		return nil, mapErr(err)
	}
	n.CreatedBy = int64Ptr(createdBy)
	n.CreatedAt = time.Unix(created, 0).UTC()
	n.UpdatedAt = time.Unix(updated, 0).UTC()
	return &n, nil
}

// CreateNote inserts a note and sets n.ID.
func (q *Queries) CreateNote(ctx context.Context, n *Note) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO notes (group_id, title, body_md, pinned, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		n.GroupID, n.Title, n.BodyMD, n.Pinned, n.CreatedBy, n.CreatedAt.Unix(), n.UpdatedAt.Unix())
	if err != nil {
		return mapErr(err)
	}
	n.ID, err = res.LastInsertId()
	return err
}

// UpdateNote saves a note's editable fields and UpdatedAt.
func (q *Queries) UpdateNote(ctx context.Context, n *Note) error {
	return expectOne(q.db.ExecContext(ctx,
		`UPDATE notes SET title = ?, body_md = ?, pinned = ?, updated_at = ? WHERE id = ? AND group_id = ?`,
		n.Title, n.BodyMD, n.Pinned, n.UpdatedAt.Unix(), n.ID, n.GroupID))
}

// DeleteNote removes a note.
func (q *Queries) DeleteNote(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx, `DELETE FROM notes WHERE id = ? AND group_id = ?`, id, groupID))
}

// NoteByID returns a note of the group.
func (q *Queries) NoteByID(ctx context.Context, groupID, id int64) (*Note, error) {
	return scanNote(q.db.QueryRowContext(ctx,
		`SELECT `+noteColumns+` FROM notes WHERE id = ? AND group_id = ?`, id, groupID))
}

// ListNotes returns the group's notes, pinned first, newest first.
func (q *Queries) ListNotes(ctx context.Context, groupID int64) ([]*Note, error) {
	return queryAll(ctx, q, scanNote,
		`SELECT `+noteColumns+` FROM notes WHERE group_id = ? ORDER BY pinned DESC, created_at DESC, id DESC`, groupID)
}
