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
	"path"
	"sort"
	"strconv"
	"strings"
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
}

// Open opens (creating if needed) the SQLite database at path.
func Open(dbPath string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	return &Store{Queries: &Queries{db: db}, db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// InTx runs fn inside a transaction, committing if fn returns nil.
func (s *Store) InTx(ctx context.Context, fn func(q *Queries) error) error {
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
