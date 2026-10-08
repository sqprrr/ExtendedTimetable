package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Every query on group content is scoped by group_id, so an id taken from
// one group's URL can never reach another group's rows.

// Subject is a row of the subjects table.
type Subject struct {
	ID        int64
	GroupID   int64
	Name      string
	ShortName string
	// Hue is the subject's colour, or '' for the default (see service.Hue).
	Hue string
	// Lecturer teaches the lectures and Instructor the practice classes and
	// labs; DLURL is the subject's distance-learning page. '' when not set.
	Lecturer   string
	Instructor string
	DLURL      string
}

const subjectColumns = `id, group_id, name, short_name, hue, lecturer, instructor, dl_url`

func scanSubject(row interface{ Scan(...any) error }) (*Subject, error) {
	var s Subject
	if err := row.Scan(&s.ID, &s.GroupID, &s.Name, &s.ShortName, &s.Hue, &s.Lecturer, &s.Instructor, &s.DLURL); err != nil {
		return nil, mapErr(err)
	}
	return &s, nil
}

// subjectKey is the value subject names must be unique by within a group.
func subjectKey(name string) string { return strings.ToLower(name) }

// CreateSubject inserts a subject and sets s.ID. Returns ErrConflict if the
// group already has a subject with that name, ignoring case.
func (q *Queries) CreateSubject(ctx context.Context, s *Subject) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO subjects (group_id, name, name_key, short_name, hue, lecturer, instructor, dl_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.GroupID, s.Name, subjectKey(s.Name), s.ShortName, s.Hue, s.Lecturer, s.Instructor, s.DLURL)
	if err != nil {
		return mapErr(err)
	}
	s.ID, err = res.LastInsertId()
	return err
}

// UpdateSubject saves a subject's editable fields. Returns ErrConflict like
// CreateSubject.
func (q *Queries) UpdateSubject(ctx context.Context, s *Subject) error {
	return expectOne(q.db.ExecContext(ctx,
		`UPDATE subjects SET name = ?, name_key = ?, short_name = ?, hue = ?, lecturer = ?, instructor = ?, dl_url = ?
		 WHERE id = ? AND group_id = ?`,
		s.Name, subjectKey(s.Name), s.ShortName, s.Hue, s.Lecturer, s.Instructor, s.DLURL, s.ID, s.GroupID))
}

// DeleteSubject removes a subject. Returns ErrReferenced while class links,
// homework or resource links still use it.
func (q *Queries) DeleteSubject(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx, `DELETE FROM subjects WHERE id = ? AND group_id = ?`, id, groupID))
}

// SubjectByID returns a subject of the group.
func (q *Queries) SubjectByID(ctx context.Context, groupID, id int64) (*Subject, error) {
	return scanSubject(q.db.QueryRowContext(ctx,
		`SELECT `+subjectColumns+` FROM subjects WHERE id = ? AND group_id = ?`, id, groupID))
}

// ListSubjects returns the group's subjects ordered by name.
func (q *Queries) ListSubjects(ctx context.Context, groupID int64) ([]*Subject, error) {
	return queryAll(ctx, q, scanSubject,
		`SELECT `+subjectColumns+` FROM subjects WHERE group_id = ? ORDER BY name COLLATE NOCASE`, groupID)
}

// queryAll runs a query and scans every row with scan.
func queryAll[T any](ctx context.Context, q *Queries, scan func(interface{ Scan(...any) error }) (*T, error), query string, args ...any) ([]*T, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func nullUnix(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func unixPtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.Unix(n.Int64, 0).UTC()
	return &t
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func int64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
