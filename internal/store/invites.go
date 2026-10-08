package store

import (
	"context"
	"database/sql"
	"time"
)

// Invite is a row of the group_invites table: the link that lets people
// join a group, /join/<Token>. A group has one at a time.
type Invite struct {
	GroupID   int64
	Token     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

const inviteColumns = `group_id, token, expires_at, created_at`

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	var inv Invite
	var expires, created int64
	if err := row.Scan(&inv.GroupID, &inv.Token, &expires, &created); err != nil {
		return nil, mapErr(err)
	}
	inv.ExpiresAt = time.Unix(expires, 0).UTC()
	inv.CreatedAt = time.Unix(created, 0).UTC()
	return &inv, nil
}

// SetInvite stores a group's invite, replacing the one it had.
func (q *Queries) SetInvite(ctx context.Context, inv *Invite) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO group_invites (group_id, token, expires_at, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (group_id) DO UPDATE SET
		   token = excluded.token, expires_at = excluded.expires_at, created_at = excluded.created_at`,
		inv.GroupID, inv.Token, inv.ExpiresAt.Unix(), inv.CreatedAt.Unix())
	return mapErr(err)
}

// InviteByGroup returns a group's invite.
func (q *Queries) InviteByGroup(ctx context.Context, groupID int64) (*Invite, error) {
	return scanInvite(q.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM group_invites WHERE group_id = ?`, groupID))
}

// InviteByToken returns the invite with the given token, expired or not.
func (q *Queries) InviteByToken(ctx context.Context, token string) (*Invite, error) {
	return scanInvite(q.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM group_invites WHERE token = ?`, token))
}

// LogEvent is what a group_log entry records.
type LogEvent string

const (
	LogGroupCreated      LogEvent = "group_created"
	LogJoined            LogEvent = "joined"
	LogLeft              LogEvent = "left"
	LogRemoved           LogEvent = "removed"
	LogLeaderClaimed     LogEvent = "leader_claimed"
	LogLeaderResigned    LogEvent = "leader_resigned"
	LogLeaderAssigned    LogEvent = "leader_assigned"
	LogLeaderRemoved     LogEvent = "leader_removed"
	LogEditorGranted     LogEvent = "editor_granted"
	LogEditorRevoked     LogEvent = "editor_revoked"
	LogInviteRegenerated LogEvent = "invite_regenerated"
)

// LogEntry is a row of the group_log table.
type LogEntry struct {
	ID      int64
	GroupID int64
	Event   LogEvent
	// ActorID is who did it; nil for the server CLI.
	ActorID *int64
	// UserID is whom it was done to, if anyone.
	UserID    *int64
	CreatedAt time.Time
	// ActorName and UserName are read from users; writes ignore them.
	ActorName string
	UserName  string
}

// AddLogEntry appends an entry to a group's log.
func (q *Queries) AddLogEntry(ctx context.Context, e *LogEntry) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO group_log (group_id, event, actor_id, user_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		e.GroupID, e.Event, e.ActorID, e.UserID, e.CreatedAt.Unix())
	if err != nil {
		return mapErr(err)
	}
	e.ID, err = res.LastInsertId()
	return err
}

func scanLogEntry(row interface{ Scan(...any) error }) (*LogEntry, error) {
	var e LogEntry
	var actor, user sql.NullInt64
	var created int64
	if err := row.Scan(&e.ID, &e.GroupID, &e.Event, &actor, &user, &created, &e.ActorName, &e.UserName); err != nil {
		return nil, mapErr(err)
	}
	e.ActorID = int64Ptr(actor)
	e.UserID = int64Ptr(user)
	e.CreatedAt = time.Unix(created, 0).UTC()
	return &e, nil
}

// GroupLog returns a group's most recent log entries, newest first.
func (q *Queries) GroupLog(ctx context.Context, groupID int64, limit int) ([]*LogEntry, error) {
	return queryAll(ctx, q, scanLogEntry,
		`SELECT l.id, l.group_id, l.event, l.actor_id, l.user_id, l.created_at,
		        COALESCE(a.username, ''), COALESCE(u.username, '')
		 FROM group_log l
		 LEFT JOIN users a ON a.id = l.actor_id
		 LEFT JOIN users u ON u.id = l.user_id
		 WHERE l.group_id = ? ORDER BY l.created_at DESC, l.id DESC LIMIT ?`, groupID, limit)
}
