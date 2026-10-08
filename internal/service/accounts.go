package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/metrics"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

const (
	minUsernameLen = 3
	maxUsernameLen = 32
	minPasswordLen = 8
)

// NewSession is a freshly issued session; Token goes into the cookie.
type NewSession struct {
	Token     string
	ExpiresAt time.Time
	// Locale is the user's chosen language, or "" if they have not chosen.
	Locale string
	// Theme is the user's chosen colour theme, or "" if they have not chosen.
	Theme string
}

// RegisterInput is the self-registration form.
type RegisterInput struct {
	Username string
	Password string
	// InviteToken is the token of the invite link of the group to join.
	InviteToken string
	ClientIP    string
	// Locale is the language the visitor chose before registering, if any;
	// an unsupported one is ignored.
	Locale string
	// Theme is the colour theme the visitor chose before registering, if
	// any; an unsupported one is ignored.
	Theme string
}

// Register creates an account through a group's invite link, makes it a
// student of that group and signs the user in.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*NewSession, error) {
	if !s.registerByIP.Allow(in.ClientIP) {
		slog.WarnContext(ctx, "registration rate limited")
		metrics.Registration(metrics.RateLimited)
		return nil, ErrRateLimited
	}
	username, err := normalizeUsername(in.Username)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	now := s.now()
	var userID int64
	var g *store.Group
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		inv, err := s.validInvite(ctx, q, in.InviteToken)
		if err != nil {
			return err
		}
		if g, err = q.GroupByID(ctx, inv.GroupID); err != nil {
			return err
		}
		u := &store.User{Username: username, PasswordHash: hash, Locale: chosenLocale(in.Locale), Theme: chosenTheme(in.Theme), CreatedAt: now}
		if err := q.CreateUser(ctx, u); errors.Is(err, store.ErrConflict) {
			return ErrUsernameTaken
		} else if err != nil {
			return err
		}
		userID = u.ID
		if err := q.AddMembership(ctx, &store.Membership{
			UserID: u.ID, GroupID: g.ID, Role: store.RoleStudent, JoinedAt: now,
		}); err != nil {
			return err
		}
		return s.logEvent(ctx, q, g.ID, store.LogJoined, ptr(u.ID), ptr(u.ID))
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "registered", "username", username, "group", g.Code)
	metrics.Registration(metrics.Success)
	sess, err := s.createSession(ctx, userID)
	if err != nil {
		return nil, err
	}
	sess.Locale = chosenLocale(in.Locale)
	sess.Theme = chosenTheme(in.Theme)
	return sess, nil
}

// LoginInput is the login form.
type LoginInput struct {
	Username string
	Password string
	ClientIP string
}

// Login checks credentials and issues a new session.
func (s *Service) Login(ctx context.Context, in LoginInput) (*NewSession, error) {
	// What was typed as a username is logged only once it is known to be an
	// account: people sometimes type their password into that field.
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if !s.loginByIP.Allow(in.ClientIP) {
		slog.WarnContext(ctx, "login rate limited", "by", "ip")
		metrics.Login(metrics.RateLimited)
		return nil, ErrRateLimited
	}
	if !s.loginByUsername.Allow(username) {
		slog.WarnContext(ctx, "login rate limited", "by", "username")
		metrics.Login(metrics.RateLimited)
		return nil, ErrRateLimited
	}
	u, err := s.store.UserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		auth.BurnPasswordCheck(in.Password)
		slog.InfoContext(ctx, "login failed", "reason", "unknown username")
		metrics.Login(metrics.Failure)
		return nil, ErrInvalidCredentials
	} else if err != nil {
		return nil, err
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		slog.InfoContext(ctx, "login failed", "reason", "wrong password", "username", u.Username)
		metrics.Login(metrics.Failure)
		return nil, ErrInvalidCredentials
	}
	s.loginByUsername.Reset(username)
	slog.InfoContext(ctx, "login", "username", u.Username)
	metrics.Login(metrics.Success)
	sess, err := s.createSession(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	sess.Locale = u.Locale
	sess.Theme = u.Theme
	return sess, nil
}

// SetLocale stores the signed-in viewer's language.
func (s *Service) SetLocale(ctx context.Context, locale string) error {
	v, err := requireViewer(ctx)
	if err != nil {
		return err
	}
	if !i18n.IsSupported(locale) {
		return inputError("locale", "err.locale")
	}
	if err := s.store.SetLocale(ctx, v.UserID, locale); err != nil {
		return err
	}
	v.Locale = locale
	return nil
}

// chosenLocale returns locale if it is supported, or "" (not chosen).
func chosenLocale(locale string) string {
	if i18n.IsSupported(locale) {
		return locale
	}
	return ""
}

// Themes are the colour themes a user can choose. ThemeSystem follows the
// device's light or dark setting.
var Themes = []string{"light", "dark", ThemeSystem}

// ThemeSystem is the theme that follows the device; it is also what a user
// who never chose sees.
const ThemeSystem = "system"

// IsTheme reports whether t is one of Themes.
func IsTheme(t string) bool { return slices.Contains(Themes, t) }

// SetTheme stores the signed-in viewer's colour theme.
func (s *Service) SetTheme(ctx context.Context, theme string) error {
	v, err := requireViewer(ctx)
	if err != nil {
		return err
	}
	if !IsTheme(theme) {
		return inputError("theme", "err.theme")
	}
	if err := s.store.SetTheme(ctx, v.UserID, theme); err != nil {
		return err
	}
	v.Theme = theme
	return nil
}

// chosenTheme returns theme if it is one of Themes, or "" (not chosen).
func chosenTheme(theme string) string {
	if IsTheme(theme) {
		return theme
	}
	return ""
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
		Theme:        u.Theme,
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
		return "", inputError("username", "err.username_length", "Min", minUsernameLen, "Max", maxUsernameLen)
	}
	for i, r := range u {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '_' || r == '.' || r == '-') && i > 0:
		default:
			return "", inputError("username", "err.username_chars")
		}
	}
	return u, nil
}

func validatePassword(p string) error {
	if utf8.RuneCountInString(p) < minPasswordLen {
		return inputError("password", "err.password_short", "Count", minPasswordLen)
	}
	if len(p) > auth.MaxPasswordBytes {
		return inputError("password", "err.password_long")
	}
	return nil
}
