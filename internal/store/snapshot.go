package store

import (
	"context"
	"database/sql"
	"sync"
)

// Each query run outside a transaction is a read transaction of its own,
// and starting one locks and unlocks the WAL index with system calls. A
// page runs a dozen queries, so a request that only reads runs them in one
// read transaction instead: a snapshot (WithSnapshot).
//
// A write ends the snapshot first, so the request's later reads, in a new
// snapshot, see it.

type snapshotKey struct{}

type snapshot struct {
	ctx context.Context
	db  *sql.DB
	gen func() uint64

	mu sync.Mutex
	tx *sql.Tx
	// version is Version when tx began.
	version uint64
	// done is set when the request is over: later queries (from anything
	// that outlived it) run on their own.
	done bool
}

// WithSnapshot returns a context whose queries run in one read transaction,
// begun at the first, and the function that ends it. The context's writes
// still work. Use it for requests that mostly read; end must be called.
func (s *Store) WithSnapshot(ctx context.Context) (_ context.Context, end func()) {
	sn := &snapshot{db: s.db, gen: s.version.Load}
	sn.ctx = context.WithValue(ctx, snapshotKey{}, sn)
	return sn.ctx, func() {
		sn.mu.Lock()
		defer sn.mu.Unlock()
		sn.rollback()
		sn.done = true
	}
}

func snapshotFrom(ctx context.Context) *snapshot {
	sn, _ := ctx.Value(snapshotKey{}).(*snapshot)
	return sn
}

// txFor is the read transaction ctx's queries run in, begun if need be, or
// nil when they run on their own.
func txFor(ctx context.Context) *sql.Tx {
	sn := snapshotFrom(ctx)
	if sn == nil {
		return nil
	}
	sn.mu.Lock()
	defer sn.mu.Unlock()
	if sn.done {
		return nil
	}
	if sn.tx == nil {
		// Read before BEGIN, so the snapshot holds every write it counts.
		version := sn.gen()
		tx, err := sn.db.BeginTx(sn.ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil
		}
		sn.tx, sn.version = tx, version
	}
	return sn.tx
}

// endSnapshot ends ctx's read transaction before a write, if it has one.
func endSnapshot(ctx context.Context) {
	if sn := snapshotFrom(ctx); sn != nil {
		sn.mu.Lock()
		sn.rollback()
		sn.mu.Unlock()
	}
}

func (sn *snapshot) rollback() {
	if sn.tx != nil {
		sn.tx.Rollback()
		sn.tx = nil
	}
}

// Version changes after every write this process makes to the database, so
// a value ctx reads may be cached under Version(ctx) until it changes. In a
// snapshot, it is the version the snapshot began at. Writes made by other
// processes (the admin CLI) do not change it.
func (s *Store) Version(ctx context.Context) uint64 {
	if sn := snapshotFrom(ctx); sn != nil {
		sn.mu.Lock()
		defer sn.mu.Unlock()
		if sn.tx != nil {
			return sn.version
		}
	}
	return s.version.Load()
}
