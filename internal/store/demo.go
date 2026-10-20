package store

import "context"

// ClearGroupContent deletes what a group's leader can change: its homework
// (with its links and everyone's progress), notes, class links, recordings
// and solutions, YouTube playlists, subjects, and the timetable cached from
// CIST (whose subjects only the next sync makes again). Its members and
// invite link stay. It is how the demo group is reset.
func (q *Queries) ClearGroupContent(ctx context.Context, groupID int64) error {
	// Subjects last: the rows before them refer to them.
	for _, table := range []string{"homework", "notes", "class_links", "resource_links",
		"youtube_videos", "youtube_playlists", "youtube_syncs", "schedule_events", "schedule_syncs", "subjects"} {
		if _, err := q.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE group_id = ?`, groupID); err != nil {
			return err
		}
	}
	return nil
}
