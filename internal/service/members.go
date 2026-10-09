package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Groups are joined only through their invite link, /join/<token>. A user
// belongs to at most one group. A group has at most one leader: while it has
// none, any member may claim the role; the leader may give it up, and a
// superadmin may hand it to another member or take it away. The leader and
// superadmins see the link, replace it, remove members (which replaces the
// link too) and make students editors or students again. Editors change the
// group's content like the leader. Every change is written to the group log,
// which only superadmins read.

// InviteTTL is how long an invite link works.
const InviteTTL = 10 * 24 * time.Hour

// groupLogLimit is how many log entries the members page shows.
const groupLogLimit = 200

var (
	// ErrInviteInvalid is returned for an unknown, replaced or expired invite link.
	ErrInviteInvalid = errors.New("this invite link is invalid or has expired")
	// ErrLeaderTaken is returned when claiming the role of a group that has a leader.
	ErrLeaderTaken = errors.New("the group already has a leader")
	// ErrLeaderMustResign is returned when the leader tries to leave the group.
	ErrLeaderMustResign = errors.New("give up the leader role before leaving the group")
)

// newInvite gives a group a fresh invite link, replacing the old one.
func (s *Service) newInvite(ctx context.Context, q *store.Queries, groupID int64) (*store.Invite, error) {
	now := s.now()
	inv := &store.Invite{GroupID: groupID, Token: auth.NewToken(), ExpiresAt: now.Add(InviteTTL), CreatedAt: now}
	return inv, q.SetInvite(ctx, inv)
}

// validInvite returns the invite with token if it still works.
func (s *Service) validInvite(ctx context.Context, q *store.Queries, token string) (*store.Invite, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInviteInvalid
	}
	inv, err := q.InviteByToken(ctx, token)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrInviteInvalid
	} else if err != nil {
		return nil, err
	}
	if !s.now().Before(inv.ExpiresAt) {
		return nil, ErrInviteInvalid
	}
	return inv, nil
}

// logEvent writes to a group's log. actor is nil for the server CLI.
func (s *Service) logEvent(ctx context.Context, q *store.Queries, groupID int64, event store.LogEvent, actor, user *int64) error {
	return q.AddLogEntry(ctx, &store.LogEntry{GroupID: groupID, Event: event, ActorID: actor, UserID: user, CreatedAt: s.now()})
}

// actorOf is the log actor for a signed-in viewer.
func actorOf(v *Viewer) *int64 {
	id := v.UserID
	return &id
}

func ptr(id int64) *int64 { return &id }

// InviteView is what the invite page shows.
type InviteView struct {
	Group     *store.Group
	ExpiresAt time.Time
	// Member is true when the signed-in viewer is already in this group.
	Member bool
	// OtherGroup is the group the signed-in viewer is in instead, if any;
	// they cannot join this one.
	OtherGroup *store.Group
}

// Invite looks up an invite link. Anyone may open it, signed in or not.
func (s *Service) Invite(ctx context.Context, token string) (*InviteView, error) {
	inv, err := s.validInvite(ctx, s.store.Queries, token)
	if err != nil {
		return nil, err
	}
	g, err := s.store.GroupByID(ctx, inv.GroupID)
	if err != nil {
		return nil, err
	}
	out := &InviteView{Group: g, ExpiresAt: inv.ExpiresAt}
	if v := ViewerFrom(ctx); v != nil {
		for _, m := range v.Memberships {
			if m.GroupID == g.ID {
				out.Member = true
			} else if out.OtherGroup == nil {
				if out.OtherGroup, err = s.store.GroupByID(ctx, m.GroupID); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

// JoinGroup makes the signed-in viewer a student of the invite's group.
// Joining a group one is already in does nothing.
func (s *Service) JoinGroup(ctx context.Context, token string) (*store.Group, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	var g *store.Group
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		inv, err := s.validInvite(ctx, q, token)
		if err != nil {
			return err
		}
		if g, err = q.GroupByID(ctx, inv.GroupID); err != nil {
			return err
		}
		ms, err := q.MembershipsByUser(ctx, v.UserID)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if m.GroupID == g.ID {
				return nil
			}
			other, err := q.GroupByID(ctx, m.GroupID)
			if err != nil {
				return err
			}
			return inputError("group", "err.in_other_group", "Group", other.Name)
		}
		if err := q.AddMembership(ctx, &store.Membership{
			UserID: v.UserID, GroupID: g.ID, Role: store.RoleStudent, JoinedAt: s.now(),
		}); err != nil {
			return err
		}
		return s.logEvent(ctx, q, g.ID, store.LogJoined, actorOf(v), actorOf(v))
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "joined group", "username", v.Username, "group", g.Code)
	return g, nil
}

// GroupInvite returns a group's invite link. The group's leader and
// superadmins only.
func (s *Service) GroupInvite(ctx context.Context, groupID int64) (*store.Invite, error) {
	if _, err := canManage(ctx, groupID); err != nil {
		return nil, err
	}
	return s.store.InviteByGroup(ctx, groupID)
}

// RegenerateInvite replaces a group's invite link; the old one stops working
// at once. The group's leader and superadmins only.
func (s *Service) RegenerateInvite(ctx context.Context, groupID int64) (*store.Invite, error) {
	v, err := canManage(ctx, groupID)
	if err != nil {
		return nil, err
	}
	var inv *store.Invite
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if inv, err = s.newInvite(ctx, q, groupID); err != nil {
			return err
		}
		return s.logEvent(ctx, q, groupID, store.LogInviteRegenerated, actorOf(v), nil)
	})
	return inv, err
}

// Members lists a group's members, the leader first, then the editors.
// Members of the group and superadmins only.
func (s *Service) Members(ctx context.Context, groupID int64) ([]*store.Member, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	return s.store.ListMembers(ctx, groupID)
}

// ClaimLeadership makes the viewer the leader of a group that has none.
func (s *Service) ClaimLeadership(ctx context.Context, groupID int64) error {
	v, err := requireViewer(ctx)
	if err != nil {
		return err
	}
	role, ok := v.RoleIn(groupID)
	if !ok {
		return ErrForbidden
	}
	if role == store.RoleLeader {
		return nil
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if _, err := q.LeaderOf(ctx, groupID); err == nil {
			return ErrLeaderTaken
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := q.SetRole(ctx, v.UserID, groupID, store.RoleLeader); errors.Is(err, store.ErrConflict) {
			return ErrLeaderTaken
		} else if err != nil {
			return notFound(err)
		}
		return s.logEvent(ctx, q, groupID, store.LogLeaderClaimed, actorOf(v), actorOf(v))
	})
	if err == nil {
		slog.InfoContext(ctx, "leader role claimed", "username", v.Username, "group_id", groupID)
	}
	return err
}

// ResignLeadership makes the viewer, the group's leader, a student again.
// The group is left without a leader until someone claims the role.
func (s *Service) ResignLeadership(ctx context.Context, groupID int64) error {
	v, err := requireViewer(ctx)
	if err != nil {
		return err
	}
	if role, _ := v.RoleIn(groupID); role != store.RoleLeader {
		return ErrForbidden
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := q.SetRole(ctx, v.UserID, groupID, store.RoleStudent); err != nil {
			return notFound(err)
		}
		return s.logEvent(ctx, q, groupID, store.LogLeaderResigned, actorOf(v), actorOf(v))
	})
	if err == nil {
		slog.InfoContext(ctx, "leader role given up", "username", v.Username, "group_id", groupID)
	}
	return err
}

// LeaveGroup takes the viewer out of a group. The leader must give up the
// role first.
func (s *Service) LeaveGroup(ctx context.Context, groupID int64) error {
	v, err := requireViewer(ctx)
	if err != nil {
		return err
	}
	role, ok := v.RoleIn(groupID)
	if !ok {
		return ErrForbidden
	}
	if role == store.RoleLeader {
		return ErrLeaderMustResign
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := q.DeleteMembership(ctx, v.UserID, groupID); err != nil {
			return notFound(err)
		}
		return s.logEvent(ctx, q, groupID, store.LogLeft, actorOf(v), actorOf(v))
	})
	if err == nil {
		slog.InfoContext(ctx, "left group", "username", v.Username, "group_id", groupID)
	}
	return err
}

// RemoveMember takes a member out of a group and replaces the invite link,
// so they cannot come straight back. The group's leader and superadmins
// only; only a superadmin may remove the leader, and nobody removes
// themselves (they leave instead).
func (s *Service) RemoveMember(ctx context.Context, groupID, userID int64) error {
	v, err := canManage(ctx, groupID)
	if err != nil {
		return err
	}
	if userID == v.UserID {
		return ErrForbidden
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		m, err := q.Membership(ctx, userID, groupID)
		if err != nil {
			return notFound(err)
		}
		if m.Role == store.RoleLeader && !v.IsSuperadmin {
			return ErrForbidden
		}
		if err := q.DeleteMembership(ctx, userID, groupID); err != nil {
			return err
		}
		if err := s.logEvent(ctx, q, groupID, store.LogRemoved, actorOf(v), ptr(userID)); err != nil {
			return err
		}
		if _, err := s.newInvite(ctx, q, groupID); err != nil {
			return err
		}
		return s.logEvent(ctx, q, groupID, store.LogInviteRegenerated, actorOf(v), nil)
	})
	if err == nil {
		slog.InfoContext(ctx, "member removed", "by", v.Username, "user_id", userID, "group_id", groupID)
	}
	return err
}

// GrantEditor makes a student of the group an editor. The group's leader and
// superadmins only.
func (s *Service) GrantEditor(ctx context.Context, groupID, userID int64) error {
	return s.changeEditor(ctx, groupID, userID, store.RoleStudent, store.RoleEditor, store.LogEditorGranted)
}

// RevokeEditor makes an editor of the group a student again. The group's
// leader and superadmins only.
func (s *Service) RevokeEditor(ctx context.Context, groupID, userID int64) error {
	return s.changeEditor(ctx, groupID, userID, store.RoleEditor, store.RoleStudent, store.LogEditorRevoked)
}

// changeEditor moves a member from role from to role to. Doing it again
// does nothing; a member in another role is left alone.
func (s *Service) changeEditor(ctx context.Context, groupID, userID int64, from, to store.Role, event store.LogEvent) error {
	v, err := canManage(ctx, groupID)
	if err != nil {
		return err
	}
	changed := false
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		m, err := q.Membership(ctx, userID, groupID)
		if err != nil {
			return notFound(err)
		}
		if m.Role == to {
			return nil
		}
		if m.Role != from {
			return ErrForbidden
		}
		if err := q.SetRole(ctx, userID, groupID, to); err != nil {
			return err
		}
		changed = true
		return s.logEvent(ctx, q, groupID, event, actorOf(v), ptr(userID))
	})
	if err == nil && changed {
		slog.InfoContext(ctx, "role changed", "by", v.Username, "user_id", userID, "group_id", groupID, "role", to)
	}
	return err
}

// SetLeader makes a member, student or editor, the group's leader; the
// previous leader becomes a student. Superadmins only.
func (s *Service) SetLeader(ctx context.Context, groupID, userID int64) error {
	v, err := requireSuperadmin(ctx)
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		return s.setLeader(ctx, q, groupID, userID, actorOf(v))
	})
}

func (s *Service) setLeader(ctx context.Context, q *store.Queries, groupID, userID int64, actor *int64) error {
	m, err := q.Membership(ctx, userID, groupID)
	if err != nil {
		return notFound(err)
	}
	if m.Role == store.RoleLeader {
		return nil
	}
	if err := s.removeLeader(ctx, q, groupID, actor); err != nil {
		return err
	}
	if err := q.SetRole(ctx, userID, groupID, store.RoleLeader); err != nil {
		return err
	}
	slog.InfoContext(ctx, "leader assigned", "user_id", userID, "group_id", groupID)
	return s.logEvent(ctx, q, groupID, store.LogLeaderAssigned, actor, ptr(userID))
}

// RemoveLeader makes the group's leader a student, leaving the group without
// a leader until someone claims the role. Superadmins only.
func (s *Service) RemoveLeader(ctx context.Context, groupID int64) error {
	v, err := requireSuperadmin(ctx)
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		return s.removeLeader(ctx, q, groupID, actorOf(v))
	})
}

// removeLeader demotes the group's leader, if it has one.
func (s *Service) removeLeader(ctx context.Context, q *store.Queries, groupID int64, actor *int64) error {
	lead, err := q.LeaderOf(ctx, groupID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if err := q.SetRole(ctx, lead.UserID, groupID, store.RoleStudent); err != nil {
		return err
	}
	slog.InfoContext(ctx, "leader removed", "username", lead.Username, "group_id", groupID)
	return s.logEvent(ctx, q, groupID, store.LogLeaderRemoved, actor, ptr(lead.UserID))
}

// GroupLog returns a group's recent log, newest first. Superadmins only.
func (s *Service) GroupLog(ctx context.Context, groupID int64) ([]*store.LogEntry, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	return s.store.GroupLog(ctx, groupID, groupLogLimit)
}

// AdminGroup is a group as listed in the superadmins' panel.
type AdminGroup struct {
	*store.Group
	Members int
	// Leader is the leader's username, or "" if the group has none.
	Leader string
	Invite *store.Invite
}

// AdminGroups lists every group with its leader, size and invite link.
// Superadmins only.
func (s *Service) AdminGroups(ctx context.Context) ([]AdminGroup, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	groups, err := s.store.ListGroupSummaries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AdminGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, AdminGroup{Group: g.Group, Members: g.Members, Leader: g.Leader, Invite: g.Invite})
	}
	return out, nil
}

// CreateGroupInput is the panel's new-group form.
type CreateGroupInput struct {
	Code string
	Name string
	// CISTGroupID links the group to its CIST timetable; nil for none.
	CISTGroupID *int64
}

// CreateGroup creates a group with its first invite link. Superadmins only.
func (s *Service) CreateGroup(ctx context.Context, in CreateGroupInput) (*store.Group, error) {
	v, err := requireSuperadmin(ctx)
	if err != nil {
		return nil, err
	}
	return s.createGroup(ctx, in, actorOf(v))
}
