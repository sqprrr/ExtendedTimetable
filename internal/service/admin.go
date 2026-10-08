package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// The methods in this file back the `extt admin` CLI. The caller is whoever
// has shell access to the server, so they perform no permission checks and
// must never be exposed over HTTP.

// AdminCreateSuperadmin creates a new superadmin account.
func (s *Service) AdminCreateSuperadmin(ctx context.Context, username, password string) error {
	username, err := normalizeUsername(username)
	if err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	err = s.store.CreateUser(ctx, &store.User{
		Username: username, PasswordHash: hash, IsSuperadmin: true, CreatedAt: s.now(),
	})
	if errors.Is(err, store.ErrConflict) {
		return ErrUsernameTaken
	}
	return err
}

// AdminCreateGroup creates a group with its first invite link.
func (s *Service) AdminCreateGroup(ctx context.Context, code, name string, cistGroupID *int64) (*store.Group, error) {
	return s.createGroup(ctx, CreateGroupInput{Code: code, Name: name, CISTGroupID: cistGroupID}, nil)
}

// createGroup creates a group and its invite link; actor is nil for the CLI.
func (s *Service) createGroup(ctx context.Context, in CreateGroupInput, actor *int64) (*store.Group, error) {
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if code == "" || len(code) > 32 {
		return nil, inputError("code", "err.group_code_length")
	}
	for _, r := range code {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return nil, inputError("code", "err.group_code_chars")
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = code
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return nil, inputError("name", "err.too_long", "Field", i18n.M("field.name"), "Count", maxNameLen)
	}
	if in.CISTGroupID != nil && *in.CISTGroupID <= 0 {
		return nil, inputError("cist_id", "err.cist_id")
	}
	g := &store.Group{Code: code, Name: name, CISTGroupID: in.CISTGroupID, CreatedAt: s.now()}
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		if err := q.CreateGroup(ctx, g); errors.Is(err, store.ErrConflict) {
			return inputError("code", "err.group_exists", "Code", code)
		} else if err != nil {
			return err
		}
		if _, err := s.newInvite(ctx, q, g.ID); err != nil {
			return err
		}
		return s.logEvent(ctx, q, g.ID, store.LogGroupCreated, actor, nil)
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "group created", "group", g.Code)
	return g, nil
}

// AdminSetLeader makes username the leader of groupCode, adding them to the
// group if they are in none; the previous leader becomes a student.
func (s *Service) AdminSetLeader(ctx context.Context, username, groupCode string) error {
	u, g, err := s.adminLookup(ctx, username, groupCode)
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		ms, err := q.MembershipsByUser(ctx, u.ID)
		if err != nil {
			return err
		}
		member := false
		for _, m := range ms {
			if m.GroupID == g.ID {
				member = true
				continue
			}
			other, err := q.GroupByID(ctx, m.GroupID)
			if err != nil {
				return err
			}
			return fmt.Errorf("%s is a member of %s; a user can be in one group only", u.Username, other.Code)
		}
		if !member {
			if err := q.AddMembership(ctx, &store.Membership{
				UserID: u.ID, GroupID: g.ID, Role: store.RoleStudent, JoinedAt: s.now(),
			}); err != nil {
				return err
			}
			if err := s.logEvent(ctx, q, g.ID, store.LogJoined, nil, ptr(u.ID)); err != nil {
				return err
			}
		}
		return s.setLeader(ctx, q, g.ID, u.ID, nil)
	})
}

// AdminRemoveLeader makes username, the leader of groupCode, a student again.
func (s *Service) AdminRemoveLeader(ctx context.Context, username, groupCode string) error {
	u, g, err := s.adminLookup(ctx, username, groupCode)
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		if m, err := q.Membership(ctx, u.ID, g.ID); err != nil || m.Role != store.RoleLeader {
			return fmt.Errorf("%s is not the leader of %s", u.Username, g.Code)
		}
		return s.removeLeader(ctx, q, g.ID, nil)
	})
}

// AdminInvite returns a group's invite link, replacing it first if
// regenerate is set.
func (s *Service) AdminInvite(ctx context.Context, groupCode string, regenerate bool) (*store.Invite, error) {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return nil, err
	}
	if !regenerate {
		return s.store.InviteByGroup(ctx, g.ID)
	}
	var inv *store.Invite
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if inv, err = s.newInvite(ctx, q, g.ID); err != nil {
			return err
		}
		return s.logEvent(ctx, q, g.ID, store.LogInviteRegenerated, nil, nil)
	})
	return inv, err
}

// AdminGroupLog returns a group's most recent log entries, newest first.
func (s *Service) AdminGroupLog(ctx context.Context, groupCode string, limit int) ([]*store.LogEntry, error) {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return nil, err
	}
	return s.store.GroupLog(ctx, g.ID, limit)
}

// AdminResetPassword sets a new password and signs the user out everywhere.
func (s *Service) AdminResetPassword(ctx context.Context, username, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	u, err := s.store.UserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("user %q not found", username)
	} else if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		if err := q.SetPasswordHash(ctx, u.ID, hash); err != nil {
			return err
		}
		return q.DeleteUserSessions(ctx, u.ID)
	})
}

func (s *Service) adminLookup(ctx context.Context, username, groupCode string) (*store.User, *store.Group, error) {
	u, err := s.store.UserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, fmt.Errorf("user %q not found", username)
	} else if err != nil {
		return nil, nil, err
	}
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return nil, nil, err
	}
	return u, g, nil
}

func (s *Service) adminGroup(ctx context.Context, groupCode string) (*store.Group, error) {
	g, err := s.store.GroupByCode(ctx, strings.TrimSpace(groupCode))
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("group %q not found", groupCode)
	}
	return g, err
}
