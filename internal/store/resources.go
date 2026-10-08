package store

import (
	"context"
	"database/sql"
	"time"
)

// ResourceKind tells recordings from solutions.
type ResourceKind string

const (
	ResourceRecording ResourceKind = "recording"
	ResourceSolution  ResourceKind = "solution"
)

// ResourceLink is a row of the resource_links table: a link to a class
// recording or a ready-made solution. Files are never stored on the server.
type ResourceLink struct {
	ID        int64
	GroupID   int64
	SubjectID int64
	Kind      ResourceKind
	Title     string
	URL       string
	// Date is "YYYY-MM-DD", or empty when not set.
	Date string
	// LessonType is the class it belongs to, or empty when not set.
	LessonType LessonType
	CreatedBy  *int64
	CreatedAt  time.Time
	// SubjectName and SubjectHue are read from subjects; writes ignore them.
	SubjectName string
	SubjectHue  string
}

// resourceLinkSelect reads resource links with their subject's name; add a
// WHERE on r.
const resourceLinkSelect = `SELECT r.id, r.group_id, r.subject_id, r.kind, r.title, r.url, r.date, r.lesson_type,
	r.created_by, r.created_at, s.name, s.hue
	FROM resource_links r JOIN subjects s ON s.id = r.subject_id `

func scanResourceLink(row interface{ Scan(...any) error }) (*ResourceLink, error) {
	var l ResourceLink
	var date, lessonType sql.NullString
	var createdBy sql.NullInt64
	var created int64
	if err := row.Scan(&l.ID, &l.GroupID, &l.SubjectID, &l.Kind, &l.Title, &l.URL, &date, &lessonType, &createdBy, &created, &l.SubjectName, &l.SubjectHue); err != nil {
		return nil, mapErr(err)
	}
	l.Date = date.String
	l.LessonType = LessonType(lessonType.String)
	l.CreatedBy = int64Ptr(createdBy)
	l.CreatedAt = time.Unix(created, 0).UTC()
	return &l, nil
}

// CreateResourceLink inserts a resource link and sets l.ID.
func (q *Queries) CreateResourceLink(ctx context.Context, l *ResourceLink) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO resource_links (group_id, subject_id, kind, title, url, date, lesson_type, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.GroupID, l.SubjectID, l.Kind, l.Title, l.URL, nullString(l.Date), nullString(string(l.LessonType)),
		l.CreatedBy, l.CreatedAt.Unix())
	if err != nil {
		return mapErr(err)
	}
	l.ID, err = res.LastInsertId()
	return err
}

// UpdateResourceLink saves a resource link's editable fields.
func (q *Queries) UpdateResourceLink(ctx context.Context, l *ResourceLink) error {
	return expectOne(q.db.ExecContext(ctx,
		`UPDATE resource_links SET subject_id = ?, kind = ?, title = ?, url = ?, date = ?, lesson_type = ?
		 WHERE id = ? AND group_id = ?`,
		l.SubjectID, l.Kind, l.Title, l.URL, nullString(l.Date), nullString(string(l.LessonType)), l.ID, l.GroupID))
}

// DeleteResourceLink removes a resource link.
func (q *Queries) DeleteResourceLink(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx, `DELETE FROM resource_links WHERE id = ? AND group_id = ?`, id, groupID))
}

// ResourceLinkByID returns a resource link of the group.
func (q *Queries) ResourceLinkByID(ctx context.Context, groupID, id int64) (*ResourceLink, error) {
	return scanResourceLink(q.db.QueryRowContext(ctx,
		resourceLinkSelect+`WHERE r.id = ? AND r.group_id = ?`, id, groupID))
}

// ListResourceLinks returns the group's resource links, newest date first;
// undated ones after the dated ones, newest added first.
func (q *Queries) ListResourceLinks(ctx context.Context, groupID int64) ([]*ResourceLink, error) {
	return queryAll(ctx, q, scanResourceLink,
		resourceLinkSelect+`WHERE r.group_id = ?
		 ORDER BY r.date IS NULL, r.date DESC, r.created_at DESC, r.id DESC`, groupID)
}
