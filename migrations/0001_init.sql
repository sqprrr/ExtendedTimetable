-- M1: groups, users, memberships, sessions.
-- Timestamps are Unix seconds (UTC).

CREATE TABLE groups (
    id            INTEGER PRIMARY KEY,
    code          TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    name          TEXT    NOT NULL,
    cist_group_id INTEGER,
    invite_code   TEXT    NOT NULL UNIQUE,
    created_at    INTEGER NOT NULL
) STRICT;

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    is_superadmin INTEGER NOT NULL DEFAULT 0 CHECK (is_superadmin IN (0, 1)),
    locale        TEXT    NOT NULL DEFAULT 'uk',
    created_at    INTEGER NOT NULL
) STRICT;

CREATE TABLE memberships (
    user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    group_id  INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    role      TEXT    NOT NULL CHECK (role IN ('student', 'leader')),
    joined_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, group_id)
) STRICT;

CREATE INDEX memberships_group_id ON memberships (group_id);

-- id is the SHA-256 hash of the session token; the raw token only lives in the cookie.
CREATE TABLE sessions (
    id         TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX sessions_user_id ON sessions (user_id);
CREATE INDEX sessions_expires_at ON sessions (expires_at);
