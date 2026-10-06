-- M2: leader tools. Every row is scoped to a group; deleting a subject removes
-- the homework, links and resources filed under it.
-- Dates are Unix seconds (UTC), except resource_links.date (YYYY-MM-DD).

CREATE TABLE subjects (
    id         INTEGER PRIMARY KEY,
    group_id   INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    short_name TEXT    NOT NULL DEFAULT '',
    UNIQUE (group_id, name)
) STRICT;

CREATE TABLE class_links (
    id          INTEGER PRIMARY KEY,
    group_id    INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    subject_id  INTEGER NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    lesson_type TEXT    NOT NULL CHECK (lesson_type IN ('lecture', 'practice', 'lab')),
    url         TEXT    NOT NULL,
    note        TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX class_links_group_id ON class_links (group_id);

CREATE TABLE homework (
    id             INTEGER PRIMARY KEY,
    group_id       INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    subject_id     INTEGER NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    title          TEXT    NOT NULL,
    description_md TEXT    NOT NULL DEFAULT '',
    due_at         INTEGER NOT NULL,
    max_points     REAL    CHECK (max_points IS NULL OR max_points > 0),
    created_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
) STRICT;

CREATE INDEX homework_group_id ON homework (group_id, due_at);

CREATE TABLE homework_links (
    id          INTEGER PRIMARY KEY,
    homework_id INTEGER NOT NULL REFERENCES homework (id) ON DELETE CASCADE,
    title       TEXT    NOT NULL,
    url         TEXT    NOT NULL
) STRICT;

CREATE INDEX homework_links_homework_id ON homework_links (homework_id);

CREATE TABLE notes (
    id         INTEGER PRIMARY KEY,
    group_id   INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    title      TEXT    NOT NULL,
    body_md    TEXT    NOT NULL DEFAULT '',
    pinned     INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
    created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX notes_group_id ON notes (group_id);

CREATE TABLE resource_links (
    id         INTEGER PRIMARY KEY,
    group_id   INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    subject_id INTEGER NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL CHECK (kind IN ('recording', 'solution')),
    title      TEXT    NOT NULL,
    url        TEXT    NOT NULL,
    date       TEXT,
    created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX resource_links_group_id ON resource_links (group_id);
