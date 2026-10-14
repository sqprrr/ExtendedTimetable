package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// YouTubeVideo is a row of the youtube_videos table: a playlist video a sync
// placed among the group's recordings.
type YouTubeVideo struct {
	GroupID int64
	VideoID string
	// ResourceID is the recording, or nil once someone deleted it.
	ResourceID *int64
	// TitleNumber is the number in the video's title, 0 if none.
	TitleNumber int
	PublishedAt time.Time
}

// AddYouTubeVideo records a video the sync placed. Returns ErrConflict if the
// group already has it.
func (q *Queries) AddYouTubeVideo(ctx context.Context, v *YouTubeVideo) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO youtube_videos (group_id, video_id, resource_id, title_number, published_at) VALUES (?, ?, ?, ?, ?)`,
		v.GroupID, v.VideoID, v.ResourceID, v.TitleNumber, v.PublishedAt.Unix())
	return mapErr(err)
}

// YouTubeVideoIDs returns the ids of the videos the group's syncs placed,
// deleted recordings included.
func (q *Queries) YouTubeVideoIDs(ctx context.Context, groupID int64) (map[string]bool, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT video_id FROM youtube_videos WHERE group_id = ?`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// SyncedRecording is a recording that came from the playlist, with what the
// sync numbers it by.
type SyncedRecording struct {
	ResourceID  int64
	SubjectID   int64
	LessonType  LessonType
	Title       string
	Date        string
	TitleNumber int
	PublishedAt time.Time
}

// ListSyncedRecordings returns the group's recordings that came from the
// playlist and still exist.
func (q *Queries) ListSyncedRecordings(ctx context.Context, groupID int64) ([]*SyncedRecording, error) {
	return queryAll(ctx, q, func(row interface{ Scan(...any) error }) (*SyncedRecording, error) {
		var r SyncedRecording
		var date, lessonType sql.NullString
		var published int64
		if err := row.Scan(&r.ResourceID, &r.SubjectID, &lessonType, &r.Title, &date, &r.TitleNumber, &published); err != nil {
			return nil, mapErr(err)
		}
		r.LessonType = LessonType(lessonType.String)
		r.Date = date.String
		r.PublishedAt = time.Unix(published, 0).UTC()
		return &r, nil
	}, `SELECT r.id, r.subject_id, r.lesson_type, r.title, r.date, v.title_number, v.published_at
		FROM youtube_videos v JOIN resource_links r ON r.id = v.resource_id
		WHERE v.group_id = ? AND r.group_id = v.group_id`, groupID)
}

// SetResourceLinkTitle renames a resource link.
func (q *Queries) SetResourceLinkTitle(ctx context.Context, groupID, id int64, title string) error {
	return expectOne(q.db.ExecContext(ctx, `UPDATE resource_links SET title = ? WHERE id = ? AND group_id = ?`, title, id, groupID))
}

// SetGroupYouTubePlaylist sets or (with "") clears a group's playlist.
func (q *Queries) SetGroupYouTubePlaylist(ctx context.Context, groupID int64, playlistID string) error {
	return expectOne(q.db.ExecContext(ctx, `UPDATE groups SET youtube_playlist = ? WHERE id = ?`, playlistID, groupID))
}

// RecordingSync is a row of the youtube_syncs table: how a group's last
// playlist sync went.
type RecordingSync struct {
	GroupID       int64
	LastAttemptAt time.Time
	LastSuccessAt *time.Time
	// LastError is empty when the last attempt succeeded.
	LastError string
	// Added counts the recordings the last successful sync added.
	Added int
	// Unmatched are the titles of the videos it could not place.
	Unmatched []string
}

// RecordingSyncFor returns how the group's last playlist sync went, or
// ErrNotFound if it never ran.
func (q *Queries) RecordingSyncFor(ctx context.Context, groupID int64) (*RecordingSync, error) {
	var s RecordingSync
	var attempt int64
	var success sql.NullInt64
	var unmatched string
	err := q.db.QueryRowContext(ctx,
		`SELECT group_id, last_attempt_at, last_success_at, last_error, added, unmatched FROM youtube_syncs WHERE group_id = ?`,
		groupID).Scan(&s.GroupID, &attempt, &success, &s.LastError, &s.Added, &unmatched)
	if err != nil {
		return nil, mapErr(err)
	}
	s.LastAttemptAt = time.Unix(attempt, 0).UTC()
	s.LastSuccessAt = unixPtr(success)
	if unmatched != "" {
		s.Unmatched = strings.Split(unmatched, "\n")
	}
	return &s, nil
}

// SaveRecordingSync records how a group's playlist sync went.
func (q *Queries) SaveRecordingSync(ctx context.Context, s *RecordingSync) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO youtube_syncs (group_id, last_attempt_at, last_success_at, last_error, added, unmatched) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (group_id) DO UPDATE SET last_attempt_at = excluded.last_attempt_at,
		   last_success_at = excluded.last_success_at, last_error = excluded.last_error,
		   added = excluded.added, unmatched = excluded.unmatched`,
		s.GroupID, s.LastAttemptAt.Unix(), nullUnix(s.LastSuccessAt), s.LastError, s.Added, strings.Join(s.Unmatched, "\n"))
	return mapErr(err)
}
