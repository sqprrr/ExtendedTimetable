// Package cache keeps values read from the database in memory, so the
// queries that run on every request (the session, the group) do not.
package cache

import (
	"sync"
	"time"
)

// Cache maps keys to values read at a version of the database. A value is
// served while the database is still at that version and the value is
// younger than the cache's TTL. The TTL bounds how long a change the
// version does not see (one made by another process) goes unnoticed.
//
// It holds at most max values; when full, it starts over empty.
type Cache[K comparable, V any] struct {
	ttl time.Duration
	max int

	mu sync.Mutex
	m  map[K]entry[V]
}

type entry[V any] struct {
	v       V
	version uint64
	at      time.Time
}

// New returns an empty cache.
func New[K comparable, V any](ttl time.Duration, max int) *Cache[K, V] {
	return &Cache[K, V]{ttl: ttl, max: max, m: map[K]entry[V]{}}
}

// Get returns the value of k if it was read at version and is fresh at now.
func (c *Cache[K, V]) Get(k K, version uint64, now time.Time) (V, bool) {
	c.mu.Lock()
	e, ok := c.m[k]
	c.mu.Unlock()
	if !ok || e.version != version || now.Sub(e.at) >= c.ttl || now.Before(e.at) {
		var zero V
		return zero, false
	}
	return e.v, true
}

// Put stores the value of k, read at version (taken before reading it) at now.
func (c *Cache[K, V]) Put(k K, v V, version uint64, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[k]; !ok && len(c.m) >= c.max {
		clear(c.m)
	}
	c.m[k] = entry[V]{v: v, version: version, at: now}
}
