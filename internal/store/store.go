// Package store holds the SQLite queries. It knows nothing about permissions;
// those are enforced in the service layer.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	// ErrNotFound is returned when a queried row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict is returned when a write violates a UNIQUE or PRIMARY KEY constraint.
	ErrConflict = errors.New("store: conflict")
	// ErrReferenced is returned when a write violates a FOREIGN KEY constraint:
	// deleting a row that others still refer to, or referring to a missing row.
	ErrReferenced = errors.New("store: foreign key")
)

// dbtx is satisfied by both *sql.DB and *sql.Tx.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Queries runs queries against either the database or a transaction.
type Queries struct {
	db dbtx
}

// Store is the database handle.
type Store struct {
	*Queries
	db *sql.DB
	// version counts the writes this process committed; see Version.
	version *atomic.Uint64
}

// maxConns bounds the open connections. Each keeps its own page cache, so
// a few are enough for a small site on a small machine; they are kept open
// rather than closed and reopened under load, since opening one runs the
// pragmas and reads the schema again.
const maxConns = 4

// rootDB is the database handle of a Store's Queries. It runs reads as
// prepared statements, in the context's snapshot if any (WithSnapshot), and
// bumps version after every write made outside a transaction (InTx bumps
// it on commit).
type rootDB struct {
	*sql.DB
	version *atomic.Uint64
	stmts   *stmtCache
}

func (d rootDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	tx := txFor(ctx)
	if st := d.stmts.get(ctx, d.DB, query); st != nil {
		if tx != nil {
			st = tx.StmtContext(ctx, st)
		}
		return st.QueryContext(ctx, args...)
	}
	if tx != nil {
		return tx.QueryContext(ctx, query, args...)
	}
	return d.DB.QueryContext(ctx, query, args...)
}

func (d rootDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	tx := txFor(ctx)
	if st := d.stmts.get(ctx, d.DB, query); st != nil {
		if tx != nil {
			st = tx.StmtContext(ctx, st)
		}
		return st.QueryRowContext(ctx, args...)
	}
	if tx != nil {
		return tx.QueryRowContext(ctx, query, args...)
	}
	return d.DB.QueryRowContext(ctx, query, args...)
}

func (d rootDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	endSnapshot(ctx)
	res, err := d.DB.ExecContext(ctx, query, args...)
	d.version.Add(1)
	return res, err
}

// stmtCache keeps the reads prepared: SQLite parsing and planning a query
// again each time it runs costs about as much as running it. Each statement
// is prepared once per connection, as it is first used on it. The queries
// are the store's own constants, so there are few of them; maxStmts is a
// safeguard.
type stmtCache struct {
	mu sync.Mutex
	m  map[string]*sql.Stmt
}

const maxStmts = 256

// get returns query prepared, or nil if it could not be (it then runs
// unprepared and reports its own error).
func (c *stmtCache) get(ctx context.Context, db *sql.DB, query string) *sql.Stmt {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st, ok := c.m[query]; ok {
		return st
	}
	if len(c.m) >= maxStmts {
		return nil
	}
	st, err := db.PrepareContext(ctx, query)
	if err != nil {
		return nil
	}
	c.m[query] = st
	return st
}

// Open opens (creating if needed) the SQLite database at path.
func Open(dbPath string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	// Read pages through a memory map instead of a system call each, and
	// keep temporary tables (sorting, GROUP BY) off the disk.
	q.Add("_pragma", "mmap_size(268435456)")
	q.Add("_pragma", "temp_store(MEMORY)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	version := new(atomic.Uint64)
	return &Store{Queries: &Queries{db: rootDB{db, version, &stmtCache{m: map[string]*sql.Stmt{}}}}, db: db, version: version}, nil
}

// Optimize lets SQLite refresh the statistics its query planner uses, when
// they are out of date. It is cheap when there is nothing to do.
func (s *Store) Optimize(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `PRAGMA optimize`)
	return err
}

// Backup writes a consistent copy of the database to path with VACUUM INTO.
// It is safe while the server is running; path must not exist yet.
func (s *Store) Backup(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("backup: %s already exists", path)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("backup to %s: %w", path, err)
	}
	return nil
}

// Close closes the database, first letting SQLite update the planner's
// statistics as it recommends.
func (s *Store) Close() error {
	_ = s.Optimize(context.Background())
	return s.db.Close()
}

// InTx runs fn inside a transaction, committing if fn returns nil.
func (s *Store) InTx(ctx context.Context, fn func(q *Queries) error) error {
	// Bumped after the commit: a reader that saw the old Version and then
	// read the old rows must not cache them under the new one.
	defer s.version.Add(1)
	endSnapshot(ctx)
	return inTx(ctx, s.db, fn)
}

func inTx(ctx context.Context, db interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}, fn func(q *Queries) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&Queries{db: tx}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Migrate applies every migration in fsys that has not been applied yet.
//
// Migrations run with foreign keys off, as SQLite requires for rebuilding a
// table (the only way to drop a UNIQUE column or change a constraint):
// otherwise dropping the old table would cascade into the rows that reference
// it. Each migration must leave every foreign key valid, which is checked
// before it commits.
func (s *Store) Migrate(ctx context.Context, fsys fs.FS) (err error) {
	defer s.version.Add(1)
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return err
	}
	type migration struct {
		version int
		name    string
	}
	var ms []migration
	seen := map[int]string{}
	for _, name := range names {
		prefix, _, ok := strings.Cut(path.Base(name), "_")
		v, err := strconv.Atoi(prefix)
		if !ok || err != nil {
			return fmt.Errorf("migration %q: name must start with a number and '_'", name)
		}
		if other, dup := seen[v]; dup {
			return fmt.Errorf("migrations %q and %q share version %d", other, name, v)
		}
		seen[v] = name
		ms = append(ms, migration{v, name})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })

	// PRAGMA foreign_keys is per connection and ignored inside a transaction,
	// so pin one connection and turn it back on before returning it to the pool.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		if _, ferr := conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`); ferr != nil && err == nil {
			err = fmt.Errorf("re-enable foreign keys: %w", ferr)
		}
	}()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range ms {
		var applied bool
		if err := conn.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = ?)`, m.version,
		).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := fs.ReadFile(fsys, m.name)
		if err != nil {
			return err
		}
		err = inTx(ctx, conn, func(q *Queries) error {
			if _, err := q.db.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			if err := q.checkForeignKeys(ctx); err != nil {
				return err
			}
			_, err := q.db.ExecContext(ctx,
				`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				m.version, time.Now().Unix())
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
	}
	return nil
}

// checkForeignKeys fails if any row references a missing parent.
func (q *Queries) checkForeignKeys(ctx context.Context) error {
	rows, err := q.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fkid int64
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		return fmt.Errorf("foreign key violation: %s row %d references a missing %s", table, rowid.Int64, parent)
	}
	return rows.Err()
}

// mapErr converts driver errors into store sentinel errors.
func mapErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return fmt.Errorf("%w: %v", ErrConflict, err)
		case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
			return fmt.Errorf("%w: %v", ErrReferenced, err)
		}
	}
	return err
}
