// Package service holds the domain logic and every permission check. The web
// and api transports only parse input and render output.
package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

var (
	ErrUnauthenticated    = errors.New("not signed in")
	ErrForbidden          = errors.New("forbidden")
	ErrNotFound           = errors.New("not found")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrRateLimited        = errors.New("too many attempts, try again later")
)

// InputError reports invalid user input. Msg is safe to show to the user once
// translated; Error() gives it in English.
type InputError struct {
	Field string
	Msg   i18n.Message
}

func (e *InputError) Error() string { return e.Field + ": " + i18n.English(e.Msg) }

// inputError builds an InputError; kv is the message's template data.
func inputError(field, msgID string, kv ...any) *InputError {
	return &InputError{Field: field, Msg: i18n.M(msgID, kv...)}
}

// Config tunes the service.
type Config struct {
	// SessionTTL is how long a session lasts without activity.
	SessionTTL time.Duration
	// CIST is where schedules come from; nil turns schedule sync off.
	CIST cist.Source
	// Location is the time zone that decides what "today" is; UTC if nil.
	Location *time.Location
}

// Service is the entry point for all domain operations.
type Service struct {
	store *store.Store
	cfg   Config
	now   func() time.Time

	loginByIP       *auth.Limiter
	loginByUsername *auth.Limiter
	registerByIP    *auth.Limiter

	// syncing holds the groups whose schedule sync is running.
	syncMu  sync.Mutex
	syncing map[int64]bool
}

// New creates a Service.
func New(st *store.Store, cfg Config) *Service {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	return &Service{
		store:           st,
		cfg:             cfg,
		now:             time.Now,
		loginByIP:       auth.NewLimiter(30, 15*time.Minute),
		loginByUsername: auth.NewLimiter(10, 15*time.Minute),
		registerByIP:    auth.NewLimiter(10, time.Hour),
		syncing:         map[int64]bool{},
	}
}

// Now is the service's clock. Transports use it for "today" so they agree
// with the service about what is in progress.
func (s *Service) Now() time.Time { return s.now() }

// Location is the time zone of "today".
func (s *Service) Location() *time.Location { return s.cfg.Location }

// StartOfDay returns midnight of t's calendar day in loc.
func StartOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// RunMaintenance periodically removes expired sessions and stale rate-limit
// buckets until ctx is done.
func (s *Service) RunMaintenance(ctx context.Context, every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := s.store.DeleteExpiredSessions(ctx, s.now()); err != nil && ctx.Err() == nil {
			return err
		}
		s.loginByIP.Prune()
		s.loginByUsername.Prune()
		s.registerByIP.Prune()
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
