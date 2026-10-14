-- Recordings synced from a YouTube playlist.
--
-- groups.youtube_playlist is the id of the playlist the group's class
-- recordings are uploaded to; '' when it has none.
ALTER TABLE groups ADD COLUMN youtube_playlist TEXT NOT NULL DEFAULT '';

-- youtube_videos remembers every video a sync placed in resource_links, so a
-- recording someone deletes is not added again: resource_id is NULL then.
-- Recordings added by hand before the playlist was set are taken over by
-- their URL. title_number is the number the uploader wrote in the title
-- ("ЛК №4"); the sync numbers recordings by date and uses it only to order
-- two of the same day.
CREATE TABLE youtube_videos (
    group_id     INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    video_id     TEXT    NOT NULL,
    resource_id  INTEGER REFERENCES resource_links (id) ON DELETE SET NULL,
    title_number INTEGER NOT NULL DEFAULT 0,
    published_at INTEGER NOT NULL,
    PRIMARY KEY (group_id, video_id)
) STRICT;

CREATE INDEX youtube_videos_resource_id ON youtube_videos (resource_id);

-- One row per group: how the last playlist sync went.
CREATE TABLE youtube_syncs (
    group_id        INTEGER PRIMARY KEY REFERENCES groups (id) ON DELETE CASCADE,
    last_attempt_at INTEGER NOT NULL,
    last_success_at INTEGER,
    -- Empty when the last attempt succeeded.
    last_error      TEXT    NOT NULL DEFAULT '',
    -- Recordings the last successful sync added.
    added           INTEGER NOT NULL DEFAULT 0,
    -- Titles of the videos it could not place (unknown subject or a title
    -- without a date), one per line.
    unmatched       TEXT    NOT NULL DEFAULT ''
) STRICT;
