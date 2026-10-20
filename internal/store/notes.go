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
	// AuthorName and AuthorRole are read from users and memberships: ""
	// once the account is deleted, and no role for an author who left the
	// group or is a superadmin outside it. Writes ignore them.
	AuthorName string
	AuthorRole Role
}

// noteSelect reads notes with their author's name and role in the group;
// add a WHERE on n.
const noteSelect = `SELECT n.id, n.group_id, n.title, n.body_md, n.pinned, n.created_by, n.created_at, n.updated_at,
	COALESCE(u.username, ''), COALESCE(m.role, '')
	FROM notes n LEFT JOIN users u ON u.id = n.created_by
	LEFT JOIN memberships m ON m.user_id = n.created_by AND m.group_id = n.group_id `

func scanNote(row interface{ Scan(...any) error }) (*Note, error) {
	var n Note
	var createdBy sql.NullInt64
	var created, updated int64
	if err := row.Scan(&n.ID, &n.GroupID, &n.Title, &n.BodyMD, &n.Pinned, &createdBy, &created, &updated,
		&n.AuthorName, &n.AuthorRole); err != nil {
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
		noteSelect+`WHERE n.id = ? AND n.group_id = ?`, id, groupID))
}

// RecentNotes returns every pinned note of the group and the latest limit
// unpinned ones, in the order of ListNotes.
func (q *Queries) RecentNotes(ctx context.Context, groupID int64, limit int) ([]*Note, error) {
	return queryAll(ctx, q, scanNote,
		noteSelect+`WHERE n.group_id = ? AND (n.pinned = 1 OR n.id IN (
			SELECT id FROM notes WHERE group_id = ? AND pinned = 0 ORDER BY created_at DESC, id DESC LIMIT ?))
		 ORDER BY n.pinned DESC, n.created_at DESC, n.id DESC`, groupID, groupID, limit)
}

// ListNotes returns the group's notes, pinned first, newest first.
func (q *Queries) ListNotes(ctx context.Context, groupID int64) ([]*Note, error) {
	return queryAll(ctx, q, scanNote,
		noteSelect+`WHERE n.group_id = ? ORDER BY n.pinned DESC, n.created_at DESC, n.id DESC`, groupID)
}
