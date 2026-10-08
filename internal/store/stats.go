package store

import (
	"context"
	"database/sql"
	"time"
)

// Stats are the row counts and sizes the metrics endpoint reports.
type Stats struct {
	Users  int
	Groups int
	// SignedInUsers counts users with at least one unexpired session.
	SignedInUsers int
	Sessions      int
	Homework      int
	Notes         int
	OpenFeedback  int
	// DBBytes is the size of the main database file (without the WAL).
	DBBytes int64
	// Syncs is the last schedule sync of every group linked to CIST.
	Syncs []*GroupSync
}

// GroupSync is how a group's last schedule sync went.
type GroupSync struct {
	Code          string
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	EventCount    int
}

// Stats counts the rows the metrics endpoint reports. now decides which
// sessions are still valid.
func (q *Queries) Stats(ctx context.Context, now time.Time) (*Stats, error) {
	var s Stats
	err := q.db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM users),
		(SELECT count(*) FROM groups),
		(SELECT count(DISTINCT user_id) FROM sessions WHERE expires_at > ?1),
		(SELECT count(*) FROM sessions WHERE expires_at > ?1),
		(SELECT count(*) FROM homework),
		(SELECT count(*) FROM notes),
		(SELECT count(*) FROM feedback WHERE resolved_at IS NULL),
		(SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size())`,
		now.Unix()).Scan(&s.Users, &s.Groups, &s.SignedInUsers, &s.Sessions, &s.Homework, &s.Notes, &s.OpenFeedback, &s.DBBytes)
	if err != nil {
		return nil, mapErr(err)
	}
	s.Syncs, err = queryAll(ctx, q, func(row interface{ Scan(...any) error }) (*GroupSync, error) {
		var g GroupSync
		var attempt, success, events sql.NullInt64
		err := row.Scan(&g.Code, &attempt, &success, &events)
		g.LastAttemptAt, g.LastSuccessAt, g.EventCount = unixPtr(attempt), unixPtr(success), int(events.Int64)
		return &g, err
	}, `SELECT g.code, s.last_attempt_at, s.last_success_at, s.event_count
		FROM groups g LEFT JOIN schedule_syncs s ON s.group_id = g.id
		WHERE g.cist_group_id IS NOT NULL ORDER BY g.code`)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
