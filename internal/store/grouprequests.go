package store

import (
	"context"
	"database/sql"
	"time"
)

// GroupRequest is a row of the group_requests table: someone asking for the
// site for their group.
type GroupRequest struct {
	ID         int64
	University string
	GroupName  string
	// Telegram is a Telegram username, without the '@'.
	Telegram string
	Comment  string
	// CISTID and CISTName are the CIST group found for GroupName, or nil
	// and "" when none was.
	CISTID     *int64
	CISTName   string
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

const groupRequestSelect = `SELECT id, university, group_name, telegram, comment, cist_id, cist_name, created_at, resolved_at
	FROM group_requests `

func scanGroupRequest(row interface{ Scan(...any) error }) (*GroupRequest, error) {
	var gr GroupRequest
	var cistID, resolved sql.NullInt64
	var created int64
	if err := row.Scan(&gr.ID, &gr.University, &gr.GroupName, &gr.Telegram, &gr.Comment,
		&cistID, &gr.CISTName, &created, &resolved); err != nil {
		return nil, mapErr(err)
	}
	gr.CISTID = int64Ptr(cistID)
	gr.CreatedAt = time.Unix(created, 0).UTC()
	gr.ResolvedAt = unixPtr(resolved)
	return &gr, nil
}

// CreateGroupRequest inserts a group request and sets gr.ID.
func (q *Queries) CreateGroupRequest(ctx context.Context, gr *GroupRequest) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO group_requests (university, group_name, telegram, comment, cist_id, cist_name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		gr.University, gr.GroupName, gr.Telegram, gr.Comment, gr.CISTID, gr.CISTName, gr.CreatedAt.Unix())
	if err != nil {
		return mapErr(err)
	}
	gr.ID, err = res.LastInsertId()
	return err
}

// ListGroupRequests returns all group requests, or only the unresolved,
// newest first.
func (q *Queries) ListGroupRequests(ctx context.Context, openOnly bool) ([]*GroupRequest, error) {
	return queryAll(ctx, q, scanGroupRequest,
		groupRequestSelect+`WHERE NOT ? OR resolved_at IS NULL ORDER BY created_at DESC, id DESC`, openOnly)
}

// GroupRequestByID returns a group request, or ErrNotFound.
func (q *Queries) GroupRequestByID(ctx context.Context, id int64) (*GroupRequest, error) {
	return scanGroupRequest(q.db.QueryRowContext(ctx, groupRequestSelect+`WHERE id = ?`, id))
}

// CountOpenGroupRequests counts the unresolved group requests.
func (q *Queries) CountOpenGroupRequests(ctx context.Context) (int, error) {
	var n int
	err := q.db.QueryRowContext(ctx, `SELECT count(*) FROM group_requests WHERE resolved_at IS NULL`).Scan(&n)
	return n, err
}

// SetGroupRequestResolved marks a group request resolved at t, or open
// again when t is nil.
func (q *Queries) SetGroupRequestResolved(ctx context.Context, id int64, t *time.Time) error {
	return expectOne(q.db.ExecContext(ctx, `UPDATE group_requests SET resolved_at = ? WHERE id = ?`, nullUnix(t), id))
}

// DeleteGroupRequest removes a group request.
func (q *Queries) DeleteGroupRequest(ctx context.Context, id int64) error {
	return expectOne(q.db.ExecContext(ctx, `DELETE FROM group_requests WHERE id = ?`, id))
}
