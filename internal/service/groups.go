package service

import (
	"context"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// GroupSummary is a group as shown on the viewer's dashboard.
type GroupSummary struct {
	ID   int64
	Code string
	Name string
	// Role is empty when a superadmin sees a group they are not a member of.
	Role      store.Role
	CanManage bool
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
		out = append(out, gs)
	}
	return out, nil
}
