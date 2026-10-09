package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/metrics"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// The schedule is a cache of CIST. A sync fetches a window of events around
// now and replaces that window in the database; when CIST is down or answers
// with nothing, the previous copy stays and the error is recorded so the
// pages can say how old the data is.

const (
	// syncPast and syncAhead bound the window a sync replaces.
	syncPast  = 7 * 24 * time.Hour
	syncAhead = 120 * 24 * time.Hour
	// syncCooldown is how often a leader may sync a group by hand.
	syncCooldown = 5 * time.Minute
	// maxScheduleDays bounds how much of the schedule one read returns.
	maxScheduleDays = 62
)

// SyncCooldownMinutes is how long a leader waits between manual syncs.
const SyncCooldownMinutes = int(syncCooldown / time.Minute)

var (
	// ErrNoCISTGroup is returned when syncing a group without a CIST id.
	ErrNoCISTGroup = errors.New("this group has no CIST timetable id yet; ask the site admin to set it")
	// ErrSyncTooSoon is returned when a leader syncs again within the cooldown.
	ErrSyncTooSoon = fmt.Errorf("the schedule was synced less than %d minutes ago, try again later", SyncCooldownMinutes)
	// ErrSyncDisabled is returned when the server runs without a CIST source.
	ErrSyncDisabled = errors.New("schedule sync is turned off on this server")
	// ErrSyncRunning is returned while another sync of the same group runs.
	ErrSyncRunning = errors.New("the schedule is being synced right now, try again in a minute")
)

// SyncError is a failed fetch from CIST. It is recorded in the group's sync
// status, which the schedule page shows.
type SyncError struct{ Err error }

func (e *SyncError) Error() string { return "CIST sync failed: " + e.Err.Error() }
func (e *SyncError) Unwrap() error { return e.Err }

// lessonTypes maps CIST's lesson type abbreviations to class link types.
var lessonTypes = map[string]store.LessonType{
	"Лк": store.LessonLecture,
	"Пз": store.LessonPractice,
	"Лб": store.LessonLab,
}

// syncGroup fetches the group's events from CIST and replaces the cached
// window. Failures to fetch are recorded and returned as *SyncError. A valid
// but empty export (holidays) is a success and empties the window.
func (s *Service) syncGroup(ctx context.Context, g *store.Group) (*store.ScheduleSync, error) {
	if s.cfg.CIST == nil {
		return nil, ErrSyncDisabled
	}
	if g.CISTGroupID == nil {
		return nil, ErrNoCISTGroup
	}
	if !s.startSync(g.ID) {
		return nil, ErrSyncRunning
	}
	defer s.endSync(g.ID)
	now := s.now()
	from, to := now.Add(-syncPast), now.Add(syncAhead)
	slog.DebugContext(ctx, "schedule sync started", "group", g.Code, "cist_group", *g.CISTGroupID)
	started := time.Now()
	events, err := s.cfg.CIST.GroupEvents(ctx, *g.CISTGroupID, from, to)
	metrics.ScheduleSync(err == nil, time.Since(started))

	rec, lerr := s.store.ScheduleSyncFor(ctx, g.ID)
	if errors.Is(lerr, store.ErrNotFound) {
		rec = &store.ScheduleSync{GroupID: g.ID}
	} else if lerr != nil {
		return nil, lerr
	}
	rec.LastAttemptAt = now
	if err != nil {
		slog.WarnContext(ctx, "schedule sync failed", "group", g.Code, "err", err)
		rec.LastError = truncate(err.Error(), 500)
		if serr := s.store.SaveScheduleSync(ctx, rec); serr != nil {
			return nil, serr
		}
		return rec, &SyncError{Err: err}
	}

	err = s.store.InTx(ctx, func(q *store.Queries) error {
		subjects, err := s.subjectsForBriefs(ctx, q, g.ID, events)
		if err != nil {
			return err
		}
		rows := make([]*store.ScheduleEvent, 0, len(events))
		for _, e := range events {
			rows = append(rows, &store.ScheduleEvent{
				StartsAt: e.Start, EndsAt: e.End, SubjectID: subjects[strings.ToLower(e.Subject)],
				SubjectBrief: e.Subject, CISTType: e.Type, Room: e.Room, Groups: e.Groups,
				LessonType: lessonTypes[e.Type], SyncedAt: now,
			})
		}
		if err := q.ReplaceScheduleEvents(ctx, g.ID, from, to, rows); err != nil {
			return err
		}
		rec.LastSuccessAt, rec.LastError, rec.EventCount = &now, "", len(rows)
		return q.SaveScheduleSync(ctx, rec)
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "schedule synced", "group", g.Code, "events", rec.EventCount)
	return rec, nil
}

// startSync marks a group's sync as running; false if it already is.
func (s *Service) startSync(groupID int64) bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncing[groupID] {
		return false
	}
	s.syncing[groupID] = true
	return true
}

func (s *Service) endSync(groupID int64) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	delete(s.syncing, groupID)
}

// subjectsForBriefs finds the group's subject for every short name in events
// and returns their ids by lowercased short name (nil: leave unlinked). In
// order, a short name goes to:
//
//  1. the subject whose short name, or else name, equals it ignoring case;
//  2. the subject earlier events with that short name were linked to, so a
//     leader can rename a subject, short name included;
//  3. nothing, if earlier events had it but are unlinked: the leader deleted
//     the subject, and it stays deleted;
//  4. a new subject named after it, the first time it appears.
func (s *Service) subjectsForBriefs(ctx context.Context, q *store.Queries, groupID int64, events []cist.Event) (map[string]*int64, error) {
	existing, err := q.ListSubjects(ctx, groupID)
	if err != nil {
		return nil, err
	}
	previous, err := q.ScheduleSubjectLinks(ctx, groupID)
	if err != nil {
		return nil, err
	}
	seen, linked := map[string]bool{}, map[string]int64{}
	for brief, id := range previous {
		key := strings.ToLower(brief)
		seen[key] = true
		if id != nil {
			linked[key] = *id
		}
	}
	byShort, byName := map[string]int64{}, map[string]int64{}
	for _, sub := range existing {
		if sub.ShortName != "" {
			byShort[strings.ToLower(sub.ShortName)] = sub.ID
		}
		byName[strings.ToLower(sub.Name)] = sub.ID
	}
	ids := map[string]*int64{}
	for _, e := range events {
		key := strings.ToLower(e.Subject)
		if _, done := ids[key]; done {
			continue
		}
		name := truncate(e.Subject, maxNameLen)
		id, ok := byShort[key]
		if !ok {
			id, ok = byName[strings.ToLower(name)]
		}
		if !ok {
			id, ok = linked[key]
		}
		if !ok && seen[key] {
			ids[key] = nil
			continue
		}
		if !ok {
			sub := &store.Subject{GroupID: groupID, Name: name}
			if utf8.RuneCountInString(e.Subject) <= maxShortNameLen {
				sub.ShortName = e.Subject
			}
			if err := q.CreateSubject(ctx, sub); err != nil {
				return nil, fmt.Errorf("create subject %q from CIST: %w", e.Subject, err)
			}
			id = sub.ID
		}
		ids[key] = &id
	}
	return ids, nil
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// SyncScheduleNow syncs the group's schedule from CIST at the request of
// someone who edits the group, at most once per cooldown.
func (s *Service) SyncScheduleNow(ctx context.Context, groupID int64) (*store.ScheduleSync, error) {
	if _, err := canEdit(ctx, groupID); err != nil {
		return nil, err
	}
	g, err := s.store.GroupByID(ctx, groupID)
	if err != nil {
		return nil, notFound(err)
	}
	if rec, err := s.store.ScheduleSyncFor(ctx, groupID); err == nil && s.now().Sub(rec.LastAttemptAt) < syncCooldown {
		return nil, ErrSyncTooSoon
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return s.syncGroup(ctx, g)
}

// AdminSyncSchedule syncs a group's schedule from the admin CLI: no
// permission check and no cooldown.
func (s *Service) AdminSyncSchedule(ctx context.Context, groupCode string) (*store.ScheduleSync, error) {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return nil, err
	}
	return s.syncGroup(ctx, g)
}

// AdminSetCISTID sets (or with nil clears) the CIST timetable id of a group.
func (s *Service) AdminSetCISTID(ctx context.Context, groupCode string, cistID *int64) error {
	g, err := s.adminGroup(ctx, groupCode)
	if err != nil {
		return err
	}
	return s.store.SetGroupCISTID(ctx, g.ID, cistID)
}

// RunScheduleSync syncs every group that has a CIST id, now and then every
// interval, until ctx is done. A group synced less than half an interval ago
// (by a leader, or before a restart) is skipped, so restarts do not hammer
// CIST.
func (s *Service) RunScheduleSync(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.syncAll(ctx, every/2)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) syncAll(ctx context.Context, fresh time.Duration) {
	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "schedule sync: list groups", "err", err)
		}
		return
	}
	for _, g := range groups {
		if g.CISTGroupID == nil || ctx.Err() != nil {
			continue
		}
		if rec, err := s.store.ScheduleSyncFor(ctx, g.ID); err == nil && s.now().Sub(rec.LastAttemptAt) < fresh {
			continue
		}
		// syncGroup logs how the fetch went; only other errors are left.
		_, err := s.syncGroup(ctx, g)
		var serr *SyncError
		if err != nil && !errors.As(err, &serr) && !errors.Is(err, ErrSyncRunning) && ctx.Err() == nil {
			slog.ErrorContext(ctx, "schedule sync", "group", g.Code, "err", err)
		}
	}
}

// ScheduleEvent is a class as shown to the viewer.
type ScheduleEvent struct {
	store.ScheduleEvent
	// ClassLinks are the meeting links for the event's subject and lesson type.
	ClassLinks []*store.ClassLink
	// Now is true while the class is in progress.
	Now bool
}

// Title is the subject's name, or CIST's short name when no subject is linked.
func (e *ScheduleEvent) Title() string {
	if e.SubjectName != "" {
		return e.SubjectName
	}
	return e.SubjectBrief
}

// Schedule is a stretch of a group's timetable.
type Schedule struct {
	Events []*ScheduleEvent
	// Upcoming is the class in progress or the next one (several when
	// subgroups have classes at the same time).
	Upcoming []*ScheduleEvent
	// HasSource is false when the group has no CIST id.
	HasSource bool
	// Sync is nil until the first sync attempt.
	Sync *store.ScheduleSync
}

// IsUpcoming reports whether e is among s.Upcoming.
func (s *Schedule) IsUpcoming(e *ScheduleEvent) bool {
	for _, u := range s.Upcoming {
		if u.ID == e.ID {
			return true
		}
	}
	return false
}

// Schedule returns the group's classes that start in [from, to).
func (s *Service) Schedule(ctx context.Context, groupID int64, from, to time.Time) (*Schedule, error) {
	if _, err := canView(ctx, groupID); err != nil {
		return nil, err
	}
	// Compare calendar days: a range across a clock change is an hour
	// longer or shorter than whole days.
	if !to.After(from) || to.After(from.AddDate(0, 0, maxScheduleDays)) {
		return nil, inputError("to", "err.schedule_range", "Count", maxScheduleDays)
	}
	g, err := s.store.GroupByID(ctx, groupID)
	if err != nil {
		return nil, notFound(err)
	}
	events, err := s.store.ListScheduleEvents(ctx, groupID, from, to)
	if err != nil {
		return nil, err
	}
	links, err := s.store.ListClassLinks(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return s.scheduleView(ctx, g, events, links)
}

// scheduleView adds class links, the upcoming class and the sync status;
// links are the group's class links.
func (s *Service) scheduleView(ctx context.Context, g *store.Group, events []*store.ScheduleEvent, links []*store.ClassLink) (*Schedule, error) {
	now := s.now()
	upcoming, err := s.store.UpcomingScheduleEvents(ctx, g.ID, now)
	if err != nil {
		return nil, err
	}
	type linkKey struct {
		subject int64
		typ     store.LessonType
	}
	bySubject := map[linkKey][]*store.ClassLink{}
	for _, l := range links {
		k := linkKey{l.SubjectID, l.LessonType}
		bySubject[k] = append(bySubject[k], l)
	}
	view := func(es []*store.ScheduleEvent) []*ScheduleEvent {
		out := make([]*ScheduleEvent, 0, len(es))
		for _, e := range es {
			v := &ScheduleEvent{ScheduleEvent: *e, Now: !now.Before(e.StartsAt) && now.Before(e.EndsAt)}
			if e.SubjectID != nil && e.LessonType != "" {
				v.ClassLinks = bySubject[linkKey{*e.SubjectID, e.LessonType}]
			}
			out = append(out, v)
		}
		return out
	}
	sch := &Schedule{Events: view(events), Upcoming: view(upcoming), HasSource: g.CISTGroupID != nil}
	if sch.Sync, err = s.store.ScheduleSyncFor(ctx, g.ID); errors.Is(err, store.ErrNotFound) {
		sch.Sync = nil
	} else if err != nil {
		return nil, err
	}
	return sch, nil
}

// todaySchedule returns the group's classes of the current day in the
// request's time zone; links are the group's class links.
func (s *Service) todaySchedule(ctx context.Context, g *store.Group, links []*store.ClassLink) (*Schedule, error) {
	start := StartOfDay(s.now(), s.Location(ctx))
	events, err := s.store.ListScheduleEvents(ctx, g.ID, start, start.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	return s.scheduleView(ctx, g, events, links)
}
