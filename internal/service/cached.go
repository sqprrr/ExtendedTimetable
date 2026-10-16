package service

import (
	"context"
	"errors"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/cache"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Every page reads the viewer's session and account and the group it is
// about. Those change rarely, so they are cached until the database changes
// (store.Version). cacheTTL bounds how long a change made by the admin CLI,
// another process, takes to show.
const (
	cacheTTL  = 15 * time.Second
	cacheSize = 4096
)

// caches holds the service's caches.
type caches struct {
	// sessions maps a session ID to its viewer.
	sessions *cache.Cache[string, cachedSession]
	groups   *cache.Cache[int64, store.Group]
	// groupIDs maps a group code to its ID.
	groupIDs *cache.Cache[string, int64]
	// leaders maps a group ID to its leader's username, "" for none.
	leaders *cache.Cache[int64, string]
}

func newCaches() caches {
	return caches{
		sessions: cache.New[string, cachedSession](cacheTTL, cacheSize),
		groups:   cache.New[int64, store.Group](cacheTTL, cacheSize),
		groupIDs: cache.New[string, int64](cacheTTL, cacheSize),
		leaders:  cache.New[int64, string](cacheTTL, cacheSize),
	}
}

type cachedSession struct {
	expiresAt time.Time
	viewer    Viewer
}

// groupByID is store.GroupByID, cached. The group is the caller's to change.
func (s *Service) groupByID(ctx context.Context, id int64) (*store.Group, error) {
	version, now := s.store.Version(ctx), s.now()
	if g, ok := s.cache.groups.Get(id, version, now); ok {
		return &g, nil
	}
	g, err := s.store.GroupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.cache.groups.Put(id, *g, version, now)
	return g, nil
}

// groupByCode is store.GroupByCode, cached.
func (s *Service) groupByCode(ctx context.Context, code string) (*store.Group, error) {
	version, now := s.store.Version(ctx), s.now()
	if id, ok := s.cache.groupIDs.Get(code, version, now); ok {
		return s.groupByID(ctx, id)
	}
	g, err := s.store.GroupByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	s.cache.groupIDs.Put(code, g.ID, version, now)
	s.cache.groups.Put(g.ID, *g, version, now)
	return g, nil
}

// leaderOf is the username of a group's leader, "" when it has none.
func (s *Service) leaderOf(ctx context.Context, groupID int64) (string, error) {
	version, now := s.store.Version(ctx), s.now()
	if name, ok := s.cache.leaders.Get(groupID, version, now); ok {
		return name, nil
	}
	var name string
	if lead, err := s.store.LeaderOf(ctx, groupID); err == nil {
		name = lead.Username
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	s.cache.leaders.Put(groupID, name, version, now)
	return name, nil
}
