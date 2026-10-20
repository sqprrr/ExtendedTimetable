// Package service holds the domain logic and every permission check. The web
// and api transports only parse input and render output.
package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
	"github.com/sqprrr/ExtendedTimetable/internal/youtube"
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
	// CISTGroups finds the CIST group a group request names; nil leaves
	// requests without one.
	CISTGroups cist.GroupFinder
	// YouTube is where playlists of recordings come from; nil turns their
	// sync off.
	YouTube youtube.Source
	// DemoGroup is the code of the demo group (see demo.go); "" for none.
	DemoGroup string
	// Location is the site's time zone: the default for users who have not
	// chosen one and whose device does not say; UTC if nil.
	Location *time.Location
	// Halloween is when the pages wear the Halloween themes;
	// HalloweenSeason if zero.
	Halloween Season
}

// Service is the entry point for all domain operations.
type Service struct {
	store *store.Store
	cfg   Config
	now   func() time.Time
	// halloween is cfg.Halloween as moments.
	halloween span

	// Login attempts are limited per client network (auth.ClientNetwork),
	// per username from one network, and per username overall. The last is
	// set high: whoever reaches it locks the account's owner out too.
	loginByIP        *auth.Limiter
	loginByUserAndIP *auth.Limiter
	loginByUsername  *auth.Limiter
	registerByIP     *auth.Limiter
	feedbackByUser   *auth.Limiter
	// Group requests come from visitors: limited per client network and,
	// against a flood from many addresses, overall.
	groupRequestByIP *auth.Limiter
	groupRequestAll  *auth.Limiter

	cache caches

	// syncing holds the groups' syncs that are running.
	syncMu  sync.Mutex
	syncing map[syncKey]bool
}

// New creates a Service.
func New(st *store.Store, cfg Config) *Service {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	// Group codes are upper case; a stray space in the settings file must
	// not make the demo group a different one in different places.
	cfg.DemoGroup = strings.ToUpper(strings.TrimSpace(cfg.DemoGroup))
	if cfg.Halloween == (Season{}) {
		cfg.Halloween = HalloweenSeason
	}
	halloween, err := cfg.Halloween.span()
	if err != nil {
		// The season is written in code (HalloweenSeason) or by a test.
		panic(err)
	}
	return &Service{
		store:            st,
		cfg:              cfg,
		now:              time.Now,
		halloween:        halloween,
		loginByIP:        auth.NewLimiter(30, 15*time.Minute),
		loginByUserAndIP: auth.NewLimiter(10, 15*time.Minute),
		loginByUsername:  auth.NewLimiter(100, 15*time.Minute),
		registerByIP:     auth.NewLimiter(10, time.Hour),
		feedbackByUser:   auth.NewLimiter(10, time.Hour),
		groupRequestByIP: auth.NewLimiter(5, time.Hour),
		groupRequestAll:  auth.NewLimiter(30, time.Hour),
		syncing:          map[syncKey]bool{},
		cache:            newCaches(),
	}
}

// WithSnapshot returns a context whose reads see the database as it was at
// the first of them, which is cheaper than each one on its own, and the
// function that ends it. Writes made with the context still work, and the
// reads after them see them.
func (s *Service) WithSnapshot(ctx context.Context) (context.Context, func()) {
	return s.store.WithSnapshot(ctx)
}

// Now is the service's clock. Transports use it for "today" so they agree
// with the service about what is in progress.
func (s *Service) Now() time.Time { return s.now() }

// StartOfDay returns midnight of t's calendar day in loc.
func StartOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// RunMaintenance periodically removes expired sessions and stale rate-limit
// buckets, and lets SQLite refresh its query planner's statistics, until
// ctx is done. A failed round is logged and the next one runs
// as usual: one busy database must not stop maintenance for good.
func (s *Service) RunMaintenance(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.maintain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) maintain(ctx context.Context) {
	defer logPanic(ctx, "maintenance")
	n, err := s.store.DeleteExpiredSessions(ctx, s.now())
	if err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "maintenance: delete expired sessions", "err", err)
	}
	if n > 0 {
		slog.Debug("expired sessions removed", "count", n)
	}
	if err := s.store.Optimize(ctx); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "maintenance: optimize the database", "err", err)
	}
	s.loginByIP.Prune()
	s.loginByUserAndIP.Prune()
	s.loginByUsername.Prune()
	s.registerByIP.Prune()
	s.feedbackByUser.Prune()
	s.groupRequestByIP.Prune()
	s.groupRequestAll.Prune()
}
