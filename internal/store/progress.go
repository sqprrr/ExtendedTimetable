package store

import (
	"context"
	"database/sql"
	"time"
)

// ProgressStatus is where a student is with an assignment.
type ProgressStatus string

const (
	StatusNotStarted ProgressStatus = "not_started"
	StatusInProgress ProgressStatus = "in_progress"
	StatusDone       ProgressStatus = "done"
)

// Progress is a row of the homework_progress table: one student's status and
// grade for one assignment.
type Progress struct {
	UserID     int64
	HomeworkID int64
	Status     ProgressStatus
	// Grade is nil until the student enters one.
	Grade     *float64
	UpdatedAt time.Time
}

func scanProgress(row interface{ Scan(...any) error }) (*Progress, error) {
	var p Progress
	var grade sql.NullFloat64
	var updated int64
	if err := row.Scan(&p.UserID, &p.HomeworkID, &p.Status, &grade, &updated); err != nil {
		return nil, mapErr(err)
	}
	if grade.Valid {
		p.Grade = &grade.Float64
	}
	p.UpdatedAt = time.Unix(updated, 0).UTC()
	return &p, nil
}

// UpsertProgress saves a student's status and grade for an assignment.
func (q *Queries) UpsertProgress(ctx context.Context, p *Progress) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO homework_progress (user_id, homework_id, status, grade, updated_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (user_id, homework_id) DO UPDATE
		 SET status = excluded.status, grade = excluded.grade, updated_at = excluded.updated_at`,
		p.UserID, p.HomeworkID, p.Status, p.Grade, p.UpdatedAt.Unix())
	return mapErr(err)
}

// ProgressFor returns a student's progress on an assignment, or ErrNotFound
// if they have not touched it yet.
func (q *Queries) ProgressFor(ctx context.Context, userID, homeworkID int64) (*Progress, error) {
	return scanProgress(q.db.QueryRowContext(ctx,
		`SELECT user_id, homework_id, status, grade, updated_at FROM homework_progress
		 WHERE user_id = ? AND homework_id = ?`, userID, homeworkID))
}

// ListProgress returns a student's progress on the assignments of a group.
func (q *Queries) ListProgress(ctx context.Context, userID, groupID int64) ([]*Progress, error) {
	return queryAll(ctx, q, scanProgress,
		`SELECT p.user_id, p.homework_id, p.status, p.grade, p.updated_at
		 FROM homework_progress p JOIN homework h ON h.id = p.homework_id
		 WHERE p.user_id = ? AND h.group_id = ?`, userID, groupID)
}
