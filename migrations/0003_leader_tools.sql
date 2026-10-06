-- M2: content the group leader manages. Every row belongs to a group; rows
-- that point at a subject must point at a subject of the same group, which the
-- composite foreign keys enforce.
--
-- A subject cannot be deleted while anything still refers to it (the default
-- NO ACTION), so a leader never wipes homework by accident.

-- name_key is the lowercased name, set by the store on every write. It makes
-- names unique per group ignoring case, which COLLATE NOCASE cannot do since it
-- only folds ASCII and the names are Ukrainian.
CREATE TABLE subjects (
    id         INTEGER PRIMARY KEY,
    group_id   INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    name_key   TEXT    NOT NULL,
    short_name TEXT    NOT NULL DEFAULT '',
    UNIQUE (group_id, name_key),
    UNIQUE (group_id, id)
) STRICT;

CREATE TABLE class_links (
    id          INTEGER PRIMARY KEY,
    group_id    INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    subject_id  INTEGER NOT NULL,
    lesson_type TEXT    NOT NULL CHECK (lesson_type IN ('lecture', 'practice', 'lab')),
    url         TEXT    NOT NULL,
    note        TEXT    NOT NULL DEFAULT '',
    FOREIGN KEY (group_id, subject_id) REFERENCES subjects (group_id, id)
) STRICT;

CREATE INDEX class_links_group_subject ON class_links (group_id, subject_id);

-- due_at is Unix seconds; NULL means no deadline.
CREATE TABLE homework (
    id             INTEGER PRIMARY KEY,
    group_id       INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    subject_id     INTEGER NOT NULL,
    title          TEXT    NOT NULL,
    description_md TEXT    NOT NULL DEFAULT '',
    due_at         INTEGER,
    max_points     REAL CHECK (max_points IS NULL OR max_points > 0),
    created_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    FOREIGN KEY (group_id, subject_id) REFERENCES subjects (group_id, id)
) STRICT;

CREATE INDEX homework_group_due ON homework (group_id, due_at);
CREATE INDEX homework_group_subject ON homework (group_id, subject_id);

CREATE TABLE homework_links (
    id          INTEGER PRIMARY KEY,
    homework_id INTEGER NOT NULL REFERENCES homework (id) ON DELETE CASCADE,
    title       TEXT    NOT NULL DEFAULT '',
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

CREATE INDEX notes_group_created ON notes (group_id, pinned, created_at);

-- date is an optional calendar date, 'YYYY-MM-DD'.
CREATE TABLE resource_links (
    id         INTEGER PRIMARY KEY,
    group_id   INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    subject_id INTEGER NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('recording', 'solution')),
    title      TEXT    NOT NULL,
    url        TEXT    NOT NULL,
    date       TEXT CHECK (date IS NULL OR date = date(date)),
    created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (group_id, subject_id) REFERENCES subjects (group_id, id)
) STRICT;

CREATE INDEX resource_links_group_subject ON resource_links (group_id, subject_id);
