package store

import (
	"context"
	"database/sql"
	"time"
)

// ScheduleEvent is a row of the schedule_events table: one class from CIST.
type ScheduleEvent struct {
	ID       int64
	GroupID  int64
	StartsAt time.Time
	EndsAt   time.Time
	// SubjectID is nil when the event is not linked to a subject.
	SubjectID *int64
	// SubjectBrief, CISTType, Room and Groups are as CIST writes them.
	SubjectBrief string
	CISTType     string
	Room         string
	Groups       string
	// LessonType is CISTType as a class link lesson type, or empty.
	LessonType LessonType
	SyncedAt   time.Time
	// SubjectName is read from subjects (empty when unlinked); writes ignore it.
	SubjectName string
}

// ScheduleSync is a row of the schedule_syncs table: how the last sync of a
// group went.
type ScheduleSync struct {
	GroupID       int64
	LastAttemptAt time.Time
	// LastSuccessAt is nil until a sync succeeds.
	LastSuccessAt *time.Time
	// LastError is empty when the last attempt succeeded.
	LastError  string
	EventCount int
}

// scheduleEventSelect reads events with their subject's name; add a WHERE on e.
const scheduleEventSelect = `SELECT e.id, e.group_id, e.starts_at, e.ends_at, e.subject_id, e.subject_brief,
	e.cist_type, e.room, e.groups_text, e.lesson_type, e.synced_at, COALESCE(s.name, '')
	FROM schedule_events e LEFT JOIN subjects s ON s.id = e.subject_id `

func scanScheduleEvent(row interface{ Scan(...any) error }) (*ScheduleEvent, error) {
	var e ScheduleEvent
	var starts, ends, synced int64
	var subjectID sql.NullInt64
	var lessonType sql.NullString
	if err := row.Scan(&e.ID, &e.GroupID, &starts, &ends, &subjectID, &e.SubjectBrief,
		&e.CISTType, &e.Room, &e.Groups, &lessonType, &synced, &e.SubjectName); err != nil {
		return nil, mapErr(err)
	}
	e.StartsAt = time.Unix(starts, 0).UTC()
	e.EndsAt = time.Unix(ends, 0).UTC()
	e.SubjectID = int64Ptr(subjectID)
	e.LessonType = LessonType(lessonType.String)
	e.SyncedAt = time.Unix(synced, 0).UTC()
	return &e, nil
}

// ReplaceScheduleEvents deletes the group's events that start in [from, to)
// and inserts events in their place. Run it in a transaction.
func (q *Queries) ReplaceScheduleEvents(ctx context.Context, groupID int64, from, to time.Time, events []*ScheduleEvent) error {
	if _, err := q.db.ExecContext(ctx,
		`DELETE FROM schedule_events WHERE group_id = ? AND starts_at >= ? AND starts_at < ?`,
		groupID, from.Unix(), to.Unix()); err != nil {
		return err
	}
	for _, e := range events {
		res, err := q.db.ExecContext(ctx,
			`INSERT INTO schedule_events (group_id, starts_at, ends_at, subject_id, subject_brief, cist_type, room,
			 groups_text, lesson_type, synced_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			groupID, e.StartsAt.Unix(), e.EndsAt.Unix(), e.SubjectID, e.SubjectBrief, e.CISTType, e.Room,
			e.Groups, nullString(string(e.LessonType)), e.SyncedAt.Unix())
		if err != nil {
			return mapErr(err)
		}
		e.GroupID = groupID
		if e.ID, err = res.LastInsertId(); err != nil {
			return err
		}
	}
	return nil
}

// ListScheduleEvents returns the group's events that start in [from, to),
// in order.
func (q *Queries) ListScheduleEvents(ctx context.Context, groupID int64, from, to time.Time) ([]*ScheduleEvent, error) {
	return queryAll(ctx, q, scanScheduleEvent,
		scheduleEventSelect+`WHERE e.group_id = ? AND e.starts_at >= ? AND e.starts_at < ?
		 ORDER BY e.starts_at, e.subject_brief, e.id`, groupID, from.Unix(), to.Unix())
}

// UpcomingScheduleEvents returns the group's first events that have not
// ended at now: the class in progress or the next one. Several events can
// share that start time (labs of different subgroups); those that already
// ended (a shorter class of another subgroup) are left out.
func (q *Queries) UpcomingScheduleEvents(ctx context.Context, groupID int64, now time.Time) ([]*ScheduleEvent, error) {
	return queryAll(ctx, q, scanScheduleEvent,
		scheduleEventSelect+`WHERE e.group_id = ? AND e.ends_at > ? AND e.starts_at = (
			SELECT MIN(starts_at) FROM schedule_events WHERE group_id = ? AND ends_at > ?)
		 ORDER BY e.subject_brief, e.id`, groupID, now.Unix(), groupID, now.Unix())
}

// ScheduleSubjectLinks returns, for every subject short name the group's
// stored events have, the subject those events link to (nil when they are
// unlinked, e.g. because a leader deleted the subject). Keys are as CIST
// wrote them; callers fold case.
func (q *Queries) ScheduleSubjectLinks(ctx context.Context, groupID int64) (map[string]*int64, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT subject_brief, MAX(subject_id) FROM schedule_events WHERE group_id = ? GROUP BY subject_brief`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := map[string]*int64{}
	for rows.Next() {
		var brief string
		var id sql.NullInt64
		if err := rows.Scan(&brief, &id); err != nil {
			return nil, err
		}
		links[brief] = int64Ptr(id)
	}
	return links, rows.Err()
}

// ScheduleSyncFor returns how the group's last sync went, or ErrNotFound if
// it never ran.
func (q *Queries) ScheduleSyncFor(ctx context.Context, groupID int64) (*ScheduleSync, error) {
	var s ScheduleSync
	var attempt int64
	var success sql.NullInt64
	err := q.db.QueryRowContext(ctx,
		`SELECT group_id, last_attempt_at, last_success_at, last_error, event_count FROM schedule_syncs WHERE group_id = ?`,
		groupID).Scan(&s.GroupID, &attempt, &success, &s.LastError, &s.EventCount)
	if err != nil {
		return nil, mapErr(err)
	}
	s.LastAttemptAt = time.Unix(attempt, 0).UTC()
	s.LastSuccessAt = unixPtr(success)
	return &s, nil
}

// SaveScheduleSync records how a group's sync went.
func (q *Queries) SaveScheduleSync(ctx context.Context, s *ScheduleSync) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO schedule_syncs (group_id, last_attempt_at, last_success_at, last_error, event_count) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (group_id) DO UPDATE SET last_attempt_at = excluded.last_attempt_at,
		   last_success_at = excluded.last_success_at, last_error = excluded.last_error, event_count = excluded.event_count`,
		s.GroupID, s.LastAttemptAt.Unix(), nullUnix(s.LastSuccessAt), s.LastError, s.EventCount)
	return mapErr(err)
}

// SetGroupCISTID sets or clears a group's CIST timetable id.
func (q *Queries) SetGroupCISTID(ctx context.Context, groupID int64, cistID *int64) error {
	return expectOne(q.db.ExecContext(ctx, `UPDATE groups SET cist_group_id = ? WHERE id = ?`, cistID, groupID))
}
