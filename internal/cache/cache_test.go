package cache

import (
	"testing"
	"time"
)

func TestCache(t *testing.T) {
	c := New[string, int](time.Minute, 2)
	now := time.Unix(1000, 0)
	c.Put("a", 1, 7, now)

	if v, ok := c.Get("a", 7, now.Add(30*time.Second)); !ok || v != 1 {
		t.Errorf("fresh value: %d, %v", v, ok)
	}
	if _, ok := c.Get("a", 8, now); ok {
		t.Error("a value read at another version must not be served")
	}
	if _, ok := c.Get("a", 7, now.Add(time.Minute)); ok {
		t.Error("a value as old as the TTL must not be served")
	}
	if _, ok := c.Get("a", 7, now.Add(-time.Second)); ok {
		t.Error("a value from the future (the clock went back) must not be served")
	}

	c.Put("b", 2, 7, now)
	c.Put("b", 3, 7, now) // replacing does not count against max
	if v, ok := c.Get("a", 7, now); !ok || v != 1 {
		t.Error("replacing a value must not empty the cache")
	}
	c.Put("c", 4, 7, now) // a third key: the cache starts over
	if _, ok := c.Get("a", 7, now); ok {
		t.Error("a full cache must start over")
	}
	if v, ok := c.Get("c", 7, now); !ok || v != 4 {
		t.Error("the value that filled the cache must be kept")
	}
}
