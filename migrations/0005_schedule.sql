-- M4: class schedule synced from CIST.
--
-- schedule_events is a cache of CIST: a sync replaces the events of a time
-- window and leaves the rest alone, so the site keeps showing the last good
-- copy while CIST is down. Times are Unix seconds.
--
-- subject_id links an event to the group's subject with the same short name
-- (created by the sync when missing). Deleting a subject only unlinks its
-- events, so the schedule never blocks a leader from deleting one. The FK is
-- on subject_id alone because ON DELETE SET NULL on a composite key would
-- also null group_id; the sync only ever links subjects of the same group.

CREATE TABLE schedule_events (
    id            INTEGER PRIMARY KEY,
    group_id      INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    starts_at     INTEGER NOT NULL,
    ends_at       INTEGER NOT NULL CHECK (ends_at > starts_at),
    subject_id    INTEGER REFERENCES subjects (id) ON DELETE SET NULL,
    -- As CIST writes them: subject short name, lesson type ("Лк"), room, groups.
    subject_brief TEXT    NOT NULL,
    cist_type     TEXT    NOT NULL,
    room          TEXT    NOT NULL DEFAULT '',
    groups_text   TEXT    NOT NULL DEFAULT '',
    -- cist_type mapped to the lesson types class links use; NULL for exams,
    -- consultations and other kinds.
    lesson_type   TEXT CHECK (lesson_type IS NULL OR lesson_type IN ('lecture', 'practice', 'lab')),
    synced_at     INTEGER NOT NULL
) STRICT;

CREATE INDEX schedule_events_group_starts ON schedule_events (group_id, starts_at);
CREATE INDEX schedule_events_subject_id ON schedule_events (subject_id);

-- One row per group: how the last sync went.
CREATE TABLE schedule_syncs (
    group_id        INTEGER PRIMARY KEY REFERENCES groups (id) ON DELETE CASCADE,
    last_attempt_at INTEGER NOT NULL,
    last_success_at INTEGER,
    -- Empty when the last attempt succeeded.
    last_error      TEXT    NOT NULL DEFAULT '',
    event_count     INTEGER NOT NULL DEFAULT 0
) STRICT;
