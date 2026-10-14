-- A group's recordings can come from several YouTube playlists (lectures,
-- practice classes, one per stream…). youtube_playlists replaces
-- groups.youtube_playlist; title and last_error are what the last sync saw.
CREATE TABLE youtube_playlists (
    group_id    INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    playlist_id TEXT    NOT NULL,
    -- The playlist's title on YouTube; '' until the first sync reads it.
    title       TEXT    NOT NULL DEFAULT '',
    -- Why the last sync could not read it; '' when it could.
    last_error  TEXT    NOT NULL DEFAULT '',
    added_at    INTEGER NOT NULL,
    PRIMARY KEY (group_id, playlist_id)
) STRICT;

INSERT INTO youtube_playlists (group_id, playlist_id, added_at)
SELECT id, youtube_playlist, unixepoch() FROM groups WHERE youtube_playlist != '';

ALTER TABLE groups DROP COLUMN youtube_playlist;
