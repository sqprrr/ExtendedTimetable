package store

import (
	"context"
	"database/sql"
	"time"
)

// Session is a row of the sessions table. ID is the hash of the cookie token.
type Session struct {
	ID        string
	UserID    int64
	ExpiresAt time.Time
	CreatedAt time.Time
}

// CreateSession inserts a session.
func (q *Queries) CreateSession(ctx context.Context, s *Session) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		s.ID, s.UserID, s.ExpiresAt.Unix(), s.CreatedAt.Unix())
	return mapErr(err)
}

// SessionByID returns a session regardless of expiry; callers check ExpiresAt.
func (q *Queries) SessionByID(ctx context.Context, id string) (*Session, error) {
	var s Session
	var expires, created int64
	err := q.db.QueryRowContext(ctx,
		`SELECT id, user_id, expires_at, created_at FROM sessions WHERE id = ?`, id,
	).Scan(&s.ID, &s.UserID, &expires, &created)
	if err != nil {
		return nil, mapErr(err)
	}
	s.ExpiresAt = time.Unix(expires, 0).UTC()
	s.CreatedAt = time.Unix(created, 0).UTC()
	return &s, nil
}

// DeleteSession removes a session. Deleting a missing session is not an error.
func (q *Queries) DeleteSession(ctx context.Context, id string) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteUserSessions removes all sessions of a user.
func (q *Queries) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// DeleteExpiredSessions removes sessions that expired before now and reports how many.
func (q *Queries) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := q.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// expectOne turns a zero-row update into ErrNotFound.
func expectOne(res sql.Result, err error) error {
	if err != nil {
		return mapErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
