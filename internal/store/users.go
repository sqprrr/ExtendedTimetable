package store

import (
	"context"
	"time"
)

// User is a row of the users table.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsSuperadmin bool
	Locale       string
	CreatedAt    time.Time
}

const userColumns = `id, username, password_hash, is_superadmin, locale, created_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var created int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsSuperadmin, &u.Locale, &created); err != nil {
		return nil, mapErr(err)
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	return &u, nil
}

// CreateUser inserts a user and sets u.ID. Returns ErrConflict if the username is taken.
func (q *Queries) CreateUser(ctx context.Context, u *User) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, is_superadmin, locale, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.Username, u.PasswordHash, u.IsSuperadmin, u.Locale, u.CreatedAt.Unix())
	if err != nil {
		return mapErr(err)
	}
	u.ID, err = res.LastInsertId()
	return err
}

// UserByID returns the user with the given id.
func (q *Queries) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(q.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// UserByUsername returns the user with the given username (case-insensitive).
func (q *Queries) UserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(q.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username))
}

// SetPasswordHash replaces a user's password hash.
func (q *Queries) SetPasswordHash(ctx context.Context, userID int64, hash string) error {
	return expectOne(q.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID))
}

// SetLocale stores a user's chosen language.
func (q *Queries) SetLocale(ctx context.Context, userID int64, locale string) error {
	return expectOne(q.db.ExecContext(ctx, `UPDATE users SET locale = ? WHERE id = ?`, locale, userID))
}

// SetSuperadmin sets or clears a user's superadmin flag.
func (q *Queries) SetSuperadmin(ctx context.Context, userID int64, on bool) error {
	return expectOne(q.db.ExecContext(ctx, `UPDATE users SET is_superadmin = ? WHERE id = ?`, on, userID))
}
