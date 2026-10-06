package service

import (
	"context"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Viewer is the signed-in user making a request.
type Viewer struct {
	UserID       int64
	Username     string
	IsSuperadmin bool
	Locale       string
	Memberships  []store.Membership
}

// RoleIn returns the viewer's role in a group, if they are a member.
func (v *Viewer) RoleIn(groupID int64) (store.Role, bool) {
	for _, m := range v.Memberships {
		if m.GroupID == groupID {
			return m.Role, true
		}
	}
	return "", false
}

// CanViewGroup reports whether the viewer may read a group's content: its
// members and superadmins.
func (v *Viewer) CanViewGroup(groupID int64) bool {
	if v.IsSuperadmin {
		return true
	}
	_, ok := v.RoleIn(groupID)
	return ok
}

// CanTrackGroup reports whether the viewer keeps a homework tracker in a
// group: its members, leaders included. A superadmin who is not a member
// has none.
func (v *Viewer) CanTrackGroup(groupID int64) bool {
	_, ok := v.RoleIn(groupID)
	return ok
}

// CanManageGroup reports whether the viewer may edit a group's content:
// its leaders and superadmins.
func (v *Viewer) CanManageGroup(groupID int64) bool {
	if v.IsSuperadmin {
		return true
	}
	role, ok := v.RoleIn(groupID)
	return ok && role == store.RoleLeader
}

type viewerKey struct{}

// WithViewer returns a context carrying v.
func WithViewer(ctx context.Context, v *Viewer) context.Context {
	return context.WithValue(ctx, viewerKey{}, v)
}

// ViewerFrom returns the signed-in viewer, or nil for anonymous requests.
func ViewerFrom(ctx context.Context) *Viewer {
	v, _ := ctx.Value(viewerKey{}).(*Viewer)
	return v
}

func requireViewer(ctx context.Context) (*Viewer, error) {
	v := ViewerFrom(ctx)
	if v == nil {
		return nil, ErrUnauthenticated
	}
	return v, nil
}
