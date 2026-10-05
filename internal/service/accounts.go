package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

const (
	minUsernameLen = 3
	maxUsernameLen = 32
	minPasswordLen = 8
	defaultLocale  = "uk"
)

// NewSession is a freshly issued session; Token goes into the cookie.
type NewSession struct {
	Token     string
	ExpiresAt time.Time
}

// RegisterInput is the self-registration form.
type RegisterInput struct {
	Username   string
	Password   string
	InviteCode string
	ClientIP   string
}

// Register creates a student account in the group the invite code belongs to
// and signs the user in.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*NewSession, error) {
	if !s.registerByIP.Allow(in.ClientIP) {
		return nil, ErrRateLimited
	}
	username, err := normalizeUsername(in.Username)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	invite := NormalizeInviteCode(in.InviteCode)
	if invite == "" {
		return nil, &InputError{Field: "invite_code", Msg: "enter the invite code from your group leader"}
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	now := s.now()
	var userID int64
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		g, err := q.GroupByInviteCode(ctx, invite)
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidInviteCode
		} else if err != nil {
			return err
		}
		u := &store.User{Username: username, PasswordHash: hash, Locale: defaultLocale, CreatedAt: now}
		if err := q.CreateUser(ctx, u); errors.Is(err, store.ErrConflict) {
			return ErrUsernameTaken
		} else if err != nil {
			return err
		}
		userID = u.ID
		return q.UpsertMembership(ctx, &store.Membership{
			UserID: u.ID, GroupID: g.ID, Role: store.RoleStudent, JoinedAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.createSession(ctx, userID)
}

// LoginInput is the login form.
type LoginInput struct {
	Username string
	Password string
	ClientIP string
}

// Login checks credentials and issues a new session.
func (s *Service) Login(ctx context.Context, in LoginInput) (*NewSession, error) {
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if !s.loginByIP.Allow(in.ClientIP) || !s.loginByUsername.Allow(username) {
		return nil, ErrRateLimited
	}
	u, err := s.store.UserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		auth.BurnPasswordCheck(in.Password)
		return nil, ErrInvalidCredentials
	} else if err != nil {
		return nil, err
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		return nil, ErrInvalidCredentials
	}
	s.loginByUsername.Reset(username)
	return s.createSession(ctx, u.ID)
}

// Logout revokes the session identified by token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, auth.SessionID(token))
}

// Authenticate resolves a session token to a context carrying the Viewer.
// It implements auth.Authenticator.
func (s *Service) Authenticate(ctx context.Context, token string) (context.Context, error) {
	sess, err := s.store.SessionByID(ctx, auth.SessionID(token))
	if errors.Is(err, store.ErrNotFound) {
		return nil, auth.ErrNoSession
	} else if err != nil {
		return nil, err
	}
	if !s.now().Before(sess.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, sess.ID)
		return nil, auth.ErrNoSession
	}
	u, err := s.store.UserByID(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	ms, err := s.store.MembershipsByUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return WithViewer(ctx, &Viewer{
		UserID:       u.ID,
		Username:     u.Username,
		IsSuperadmin: u.IsSuperadmin,
		Locale:       u.Locale,
		Memberships:  ms,
	}), nil
}

func (s *Service) createSession(ctx context.Context, userID int64) (*NewSession, error) {
	token := auth.NewToken()
	now := s.now()
	sess := &store.Session{
		ID:        auth.SessionID(token),
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.cfg.SessionTTL),
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return &NewSession{Token: token, ExpiresAt: sess.ExpiresAt}, nil
}

func normalizeUsername(raw string) (string, error) {
	u := strings.ToLower(strings.TrimSpace(raw))
	if len(u) < minUsernameLen || len(u) > maxUsernameLen {
		return "", &InputError{Field: "username",
			Msg: fmt.Sprintf("username must be %d–%d characters", minUsernameLen, maxUsernameLen)}
	}
	for i, r := range u {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '_' || r == '.' || r == '-') && i > 0:
		default:
			return "", &InputError{Field: "username",
				Msg: "username may contain only latin letters, digits, '_', '.', '-' and must start with a letter or digit"}
		}
	}
	return u, nil
}

func validatePassword(p string) error {
	if utf8.RuneCountInString(p) < minPasswordLen {
		return &InputError{Field: "password", Msg: fmt.Sprintf("password must be at least %d characters", minPasswordLen)}
	}
	if len(p) > auth.MaxPasswordBytes {
		return &InputError{Field: "password", Msg: "password is too long"}
	}
	return nil
}

// NormalizeInviteCode canonicalizes user input: case-insensitive, and dashes
// or spaces are optional.
func NormalizeInviteCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) != 12 {
		return s
	}
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12]
}
