package store

import (
	"context"
	"database/sql"
	"time"
)

// Homework is a row of the homework table.
type Homework struct {
	ID            int64
	GroupID       int64
	SubjectID     int64
	Title         string
	DescriptionMD string
	// DueAt is nil when the assignment has no deadline.
	DueAt *time.Time
	// MaxPoints is nil when the assignment is not graded in points.
	MaxPoints *float64
	// CreatedBy is nil once the author's account is deleted.
	CreatedBy *int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HomeworkLink is a row of the homework_links table.
type HomeworkLink struct {
	ID         int64
	HomeworkID int64
	Title      string
	URL        string
}

const homeworkColumns = `id, group_id, subject_id, title, description_md, due_at, max_points, created_by, created_at, updated_at`

func scanHomework(row interface{ Scan(...any) error }) (*Homework, error) {
	var h Homework
	var due, createdBy sql.NullInt64
	var maxPoints sql.NullFloat64
	var created, updated int64
	if err := row.Scan(&h.ID, &h.GroupID, &h.SubjectID, &h.Title, &h.DescriptionMD,
		&due, &maxPoints, &createdBy, &created, &updated); err != nil {
		return nil, mapErr(err)
	}
	h.DueAt = unixPtr(due)
	if maxPoints.Valid {
		h.MaxPoints = &maxPoints.Float64
	}
	h.CreatedBy = int64Ptr(createdBy)
	h.CreatedAt = time.Unix(created, 0).UTC()
	h.UpdatedAt = time.Unix(updated, 0).UTC()
	return &h, nil
}

// CreateHomework inserts an assignment and sets h.ID.
func (q *Queries) CreateHomework(ctx context.Context, h *Homework) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO homework (group_id, subject_id, title, description_md, due_at, max_points, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.GroupID, h.SubjectID, h.Title, h.DescriptionMD, nullUnix(h.DueAt), h.MaxPoints, h.CreatedBy,
		h.CreatedAt.Unix(), h.UpdatedAt.Unix())
	if err != nil {
		return mapErr(err)
	}
	h.ID, err = res.LastInsertId()
	return err
}

// UpdateHomework saves an assignment's editable fields and UpdatedAt.
func (q *Queries) UpdateHomework(ctx context.Context, h *Homework) error {
	return expectOne(q.db.ExecContext(ctx,
		`UPDATE homework SET subject_id = ?, title = ?, description_md = ?, due_at = ?, max_points = ?, updated_at = ?
		 WHERE id = ? AND group_id = ?`,
		h.SubjectID, h.Title, h.DescriptionMD, nullUnix(h.DueAt), h.MaxPoints, h.UpdatedAt.Unix(), h.ID, h.GroupID))
}

// DeleteHomework removes an assignment and its links.
func (q *Queries) DeleteHomework(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx, `DELETE FROM homework WHERE id = ? AND group_id = ?`, id, groupID))
}

// HomeworkByID returns an assignment of the group.
func (q *Queries) HomeworkByID(ctx context.Context, groupID, id int64) (*Homework, error) {
	return scanHomework(q.db.QueryRowContext(ctx,
		`SELECT `+homeworkColumns+` FROM homework WHERE id = ? AND group_id = ?`, id, groupID))
}

// ListHomework returns the group's assignments by due date, those without a
// deadline last.
func (q *Queries) ListHomework(ctx context.Context, groupID int64) ([]*Homework, error) {
	return queryAll(ctx, q, scanHomework,
		`SELECT `+homeworkColumns+` FROM homework WHERE group_id = ?
		 ORDER BY due_at IS NULL, due_at, id`, groupID)
}

// HomeworkLinks returns an assignment's links in the order they were added.
// Callers must have already checked that the assignment is in their group.
func (q *Queries) HomeworkLinks(ctx context.Context, homeworkID int64) ([]*HomeworkLink, error) {
	return queryAll(ctx, q, func(row interface{ Scan(...any) error }) (*HomeworkLink, error) {
		var l HomeworkLink
		if err := row.Scan(&l.ID, &l.HomeworkID, &l.Title, &l.URL); err != nil {
			return nil, err
		}
		return &l, nil
	}, `SELECT id, homework_id, title, url FROM homework_links WHERE homework_id = ? ORDER BY id`, homeworkID)
}

// ReplaceHomeworkLinks replaces all links of an assignment. Run it in a
// transaction together with the homework write.
func (q *Queries) ReplaceHomeworkLinks(ctx context.Context, homeworkID int64, links []*HomeworkLink) error {
	if _, err := q.db.ExecContext(ctx, `DELETE FROM homework_links WHERE homework_id = ?`, homeworkID); err != nil {
		return err
	}
	for _, l := range links {
		res, err := q.db.ExecContext(ctx,
			`INSERT INTO homework_links (homework_id, title, url) VALUES (?, ?, ?)`, homeworkID, l.Title, l.URL)
		if err != nil {
			return mapErr(err)
		}
		l.HomeworkID = homeworkID
		if l.ID, err = res.LastInsertId(); err != nil {
			return err
		}
	}
	return nil
}
