package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
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
		Username: username, PasswordHash: hash, IsSuperadmin: true, Locale: defaultLocale, CreatedAt: s.now(),
	})
	if errors.Is(err, store.ErrConflict) {
		return ErrUsernameTaken
	}
	return err
}

// AdminCreateGroup creates a group and returns it with its first invite code.
func (s *Service) AdminCreateGroup(ctx context.Context, code, name string, cistGroupID *int64) (*store.Group, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || len(code) > 32 {
		return nil, &InputError{Field: "code", Msg: "group code must be 1–32 characters"}
	}
	for _, r := range code {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return nil, &InputError{Field: "code", Msg: "group code may contain only latin letters, digits, '-' and '_'"}
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = code
	}
	g := &store.Group{Code: code, Name: name, CISTGroupID: cistGroupID, CreatedAt: s.now()}
	for range 5 {
		g.InviteCode = auth.NewInviteCode()
		err := s.store.CreateGroup(ctx, g)
		if err == nil {
			return g, nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return nil, err
		}
		if _, err := s.store.GroupByCode(ctx, code); err == nil {
			return nil, fmt.Errorf("group %s already exists", code)
		}
		// Otherwise the invite code collided; try another one.
	}
	return nil, errors.New("could not generate a unique invite code")
}

// AdminSetRole makes username a member of groupCode with the given role,
// adding the membership if needed.
func (s *Service) AdminSetRole(ctx context.Context, username, groupCode string, role store.Role) error {
	u, g, err := s.adminLookup(ctx, username, groupCode)
	if err != nil {
		return err
	}
	return s.store.UpsertMembership(ctx, &store.Membership{
		UserID: u.ID, GroupID: g.ID, Role: role, JoinedAt: s.now(),
	})
}

// AdminInviteCode returns a group's invite code, optionally regenerating it.
func (s *Service) AdminInviteCode(ctx context.Context, groupCode string, regenerate bool) (string, error) {
	g, err := s.store.GroupByCode(ctx, strings.TrimSpace(groupCode))
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("group %q not found", groupCode)
	} else if err != nil {
		return "", err
	}
	if !regenerate {
		return g.InviteCode, nil
	}
	return s.setNewInviteCode(ctx, g.ID)
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
	g, err := s.store.GroupByCode(ctx, strings.TrimSpace(groupCode))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, fmt.Errorf("group %q not found", groupCode)
	} else if err != nil {
		return nil, nil, err
	}
	return u, g, nil
}
