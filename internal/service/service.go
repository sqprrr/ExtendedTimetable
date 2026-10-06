// Package service holds the domain logic and every permission check. The web
// and api transports only parse input and render output.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
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

// InputError reports invalid user input; Msg is safe to show to the user.
type InputError struct {
	Field string
	Msg   string
}

func (e *InputError) Error() string { return e.Field + ": " + e.Msg }

// Config tunes the service.
type Config struct {
	// SessionTTL is how long a session lasts without activity.
	SessionTTL time.Duration
}

// Service is the entry point for all domain operations.
type Service struct {
	store *store.Store
	cfg   Config
	now   func() time.Time

	loginByIP       *auth.Limiter
	loginByUsername *auth.Limiter
	registerByIP    *auth.Limiter
}

// New creates a Service.
func New(st *store.Store, cfg Config) *Service {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	return &Service{
		store:           st,
		cfg:             cfg,
		now:             time.Now,
		loginByIP:       auth.NewLimiter(30, 15*time.Minute),
		loginByUsername: auth.NewLimiter(10, 15*time.Minute),
		registerByIP:    auth.NewLimiter(10, time.Hour),
	}
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
