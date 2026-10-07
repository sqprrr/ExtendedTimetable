package store

import (
	"context"
	"database/sql"
	"time"
)

// FeedbackKind is what a piece of feedback is about.
type FeedbackKind string

const (
	FeedbackBug    FeedbackKind = "bug"
	FeedbackIdea   FeedbackKind = "idea"
	FeedbackReview FeedbackKind = "review"
)

// Feedback is a row of the feedback table: a message from a user to the
// site's superadmins.
type Feedback struct {
	ID int64
	// UserID is nil once the author's account is deleted.
	UserID *int64
	Kind   FeedbackKind
	// Rating is 1–5 for a review that has one, otherwise nil.
	Rating     *int64
	Message    string
	CreatedAt  time.Time
	ResolvedAt *time.Time
	// Username is read from users ("" for a deleted account); writes ignore it.
	Username string
}

const feedbackSelect = `SELECT f.id, f.user_id, f.kind, f.rating, f.message, f.created_at, f.resolved_at,
	COALESCE(u.username, '') FROM feedback f LEFT JOIN users u ON u.id = f.user_id `

func scanFeedback(row interface{ Scan(...any) error }) (*Feedback, error) {
	var f Feedback
	var userID, rating, resolved sql.NullInt64
	var created int64
	if err := row.Scan(&f.ID, &userID, &f.Kind, &rating, &f.Message, &created, &resolved, &f.Username); err != nil {
		return nil, mapErr(err)
	}
	f.UserID = int64Ptr(userID)
	f.Rating = int64Ptr(rating)
	f.CreatedAt = time.Unix(created, 0).UTC()
	f.ResolvedAt = unixPtr(resolved)
	return &f, nil
}

// CreateFeedback inserts feedback and sets f.ID.
func (q *Queries) CreateFeedback(ctx context.Context, f *Feedback) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO feedback (user_id, kind, rating, message, created_at) VALUES (?, ?, ?, ?, ?)`,
		f.UserID, f.Kind, f.Rating, f.Message, f.CreatedAt.Unix())
	if err != nil {
		return mapErr(err)
	}
	f.ID, err = res.LastInsertId()
	return err
}

// FeedbackByUser returns what a user has sent, newest first.
func (q *Queries) FeedbackByUser(ctx context.Context, userID int64) ([]*Feedback, error) {
	return queryAll(ctx, q, scanFeedback,
		feedbackSelect+`WHERE f.user_id = ? ORDER BY f.created_at DESC, f.id DESC`, userID)
}

// ListFeedback returns all feedback, or only the unresolved, newest first.
func (q *Queries) ListFeedback(ctx context.Context, openOnly bool) ([]*Feedback, error) {
	return queryAll(ctx, q, scanFeedback,
		feedbackSelect+`WHERE NOT ? OR f.resolved_at IS NULL ORDER BY f.created_at DESC, f.id DESC`, openOnly)
}

// CountOpenFeedback counts the unresolved feedback.
func (q *Queries) CountOpenFeedback(ctx context.Context) (int, error) {
	var n int
	err := q.db.QueryRowContext(ctx, `SELECT count(*) FROM feedback WHERE resolved_at IS NULL`).Scan(&n)
	return n, err
}

// SetFeedbackResolved marks feedback resolved at t, or open again when t is nil.
func (q *Queries) SetFeedbackResolved(ctx context.Context, id int64, t *time.Time) error {
	return expectOne(q.db.ExecContext(ctx, `UPDATE feedback SET resolved_at = ? WHERE id = ?`, nullUnix(t), id))
}

// DeleteFeedback removes feedback.
func (q *Queries) DeleteFeedback(ctx context.Context, id int64) error {
	return expectOne(q.db.ExecContext(ctx, `DELETE FROM feedback WHERE id = ?`, id))
}
