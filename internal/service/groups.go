package service

import (
	"context"
	"errors"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// GroupSummary is a group as shown on the viewer's dashboard.
type GroupSummary struct {
	ID   int64
	Code string
	Name string
	// Role is empty when a superadmin sees a group they are not a member of.
	Role store.Role
	// InviteCode is only set when the viewer can manage the group.
	InviteCode string
	CanManage  bool
}

// MyGroups lists the viewer's groups. Superadmins see every group.
func (s *Service) MyGroups(ctx context.Context) ([]GroupSummary, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	var groups []*store.Group
	if v.IsSuperadmin {
		if groups, err = s.store.ListGroups(ctx); err != nil {
			return nil, err
		}
	} else {
		for _, m := range v.Memberships {
			g, err := s.store.GroupByID(ctx, m.GroupID)
			if err != nil {
				return nil, err
			}
			groups = append(groups, g)
		}
	}
	out := make([]GroupSummary, 0, len(groups))
	for _, g := range groups {
		gs := GroupSummary{ID: g.ID, Code: g.Code, Name: g.Name, CanManage: v.CanManageGroup(g.ID)}
		gs.Role, _ = v.RoleIn(g.ID)
		if gs.CanManage {
			gs.InviteCode = g.InviteCode
		}
		out = append(out, gs)
	}
	return out, nil
}

// RegenerateInviteCode replaces a group's invite code, invalidating the old
// one. Allowed for the group's leaders and superadmins.
func (s *Service) RegenerateInviteCode(ctx context.Context, groupID int64) (string, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return "", err
	}
	if !v.CanManageGroup(groupID) {
		return "", ErrForbidden
	}
	return s.setNewInviteCode(ctx, groupID)
}

func (s *Service) setNewInviteCode(ctx context.Context, groupID int64) (string, error) {
	// Collisions are astronomically unlikely, but retry rather than fail.
	for range 5 {
		code := auth.NewInviteCode()
		err := s.store.SetInviteCode(ctx, groupID, code)
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		if err != nil {
			return "", mapStoreErr(err)
		}
		return code, nil
	}
	return "", errors.New("could not generate a unique invite code")
}
