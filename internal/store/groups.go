package store

import (
	"context"
	"database/sql"
	"time"
)

// Group is a row of the groups table.
type Group struct {
	ID          int64
	Code        string
	Name        string
	CISTGroupID *int64
	InviteCode  string
	CreatedAt   time.Time
}

// Role is a member's role within a group.
type Role string

const (
	RoleStudent Role = "student"
	RoleLeader  Role = "leader"
)

// Membership links a user to a group.
type Membership struct {
	UserID   int64
	GroupID  int64
	Role     Role
	JoinedAt time.Time
}

const groupColumns = `id, code, name, cist_group_id, invite_code, created_at`

func scanGroup(row interface{ Scan(...any) error }) (*Group, error) {
	var g Group
	var cist sql.NullInt64
	var created int64
	if err := row.Scan(&g.ID, &g.Code, &g.Name, &cist, &g.InviteCode, &created); err != nil {
		return nil, mapErr(err)
	}
	if cist.Valid {
		g.CISTGroupID = &cist.Int64
	}
	g.CreatedAt = time.Unix(created, 0).UTC()
	return &g, nil
}

// CreateGroup inserts a group and sets g.ID. Returns ErrConflict if the code
// or invite code is taken.
func (q *Queries) CreateGroup(ctx context.Context, g *Group) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO groups (code, name, cist_group_id, invite_code, created_at) VALUES (?, ?, ?, ?, ?)`,
		g.Code, g.Name, g.CISTGroupID, g.InviteCode, g.CreatedAt.Unix())
	if err != nil {
		return mapErr(err)
	}
	g.ID, err = res.LastInsertId()
	return err
}

// GroupByID returns the group with the given id.
func (q *Queries) GroupByID(ctx context.Context, id int64) (*Group, error) {
	return scanGroup(q.db.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM groups WHERE id = ?`, id))
}

// GroupByCode returns the group with the given code (case-insensitive).
func (q *Queries) GroupByCode(ctx context.Context, code string) (*Group, error) {
	return scanGroup(q.db.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM groups WHERE code = ?`, code))
}

// GroupByInviteCode returns the group whose current invite code matches.
func (q *Queries) GroupByInviteCode(ctx context.Context, invite string) (*Group, error) {
	return scanGroup(q.db.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM groups WHERE invite_code = ?`, invite))
}

// ListGroups returns all groups ordered by code.
func (q *Queries) ListGroups(ctx context.Context) ([]*Group, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT `+groupColumns+` FROM groups ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var gs []*Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		gs = append(gs, g)
	}
	return gs, rows.Err()
}

// SetInviteCode replaces a group's invite code.
func (q *Queries) SetInviteCode(ctx context.Context, groupID int64, invite string) error {
	res, err := q.db.ExecContext(ctx, `UPDATE groups SET invite_code = ? WHERE id = ?`, invite, groupID)
	if err != nil {
		return mapErr(err)
	}
	return expectOne(res, nil)
}

// UpsertMembership adds a user to a group, or updates the role if they are
// already a member.
func (q *Queries) UpsertMembership(ctx context.Context, m *Membership) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO memberships (user_id, group_id, role, joined_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (user_id, group_id) DO UPDATE SET role = excluded.role`,
		m.UserID, m.GroupID, m.Role, m.JoinedAt.Unix())
	return mapErr(err)
}

// Membership returns a user's membership in a group.
func (q *Queries) Membership(ctx context.Context, userID, groupID int64) (*Membership, error) {
	var m Membership
	var joined int64
	err := q.db.QueryRowContext(ctx,
		`SELECT user_id, group_id, role, joined_at FROM memberships WHERE user_id = ? AND group_id = ?`,
		userID, groupID,
	).Scan(&m.UserID, &m.GroupID, &m.Role, &joined)
	if err != nil {
		return nil, mapErr(err)
	}
	m.JoinedAt = time.Unix(joined, 0).UTC()
	return &m, nil
}

// MembershipsByUser returns all memberships of a user.
func (q *Queries) MembershipsByUser(ctx context.Context, userID int64) ([]Membership, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT user_id, group_id, role, joined_at FROM memberships WHERE user_id = ? ORDER BY joined_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ms []Membership
	for rows.Next() {
		var m Membership
		var joined int64
		if err := rows.Scan(&m.UserID, &m.GroupID, &m.Role, &joined); err != nil {
			return nil, err
		}
		m.JoinedAt = time.Unix(joined, 0).UTC()
		ms = append(ms, m)
	}
	return ms, rows.Err()
}
