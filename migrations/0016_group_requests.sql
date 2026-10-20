-- Group requests: someone, usually a group leader without an account yet,
-- asks for the site for their group. Superadmins read them and get in touch
-- on Telegram. telegram is a username without the '@'; cist_id and
-- cist_name are the CIST group the form found for group_name, if any.
CREATE TABLE group_requests (
    id          INTEGER PRIMARY KEY,
    university  TEXT    NOT NULL,
    group_name  TEXT    NOT NULL,
    telegram    TEXT    NOT NULL,
    comment     TEXT    NOT NULL DEFAULT '',
    cist_id     INTEGER,
    cist_name   TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    -- resolved_at is set once a superadmin has dealt with it.
    resolved_at INTEGER
) STRICT;
