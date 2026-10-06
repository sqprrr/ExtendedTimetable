-- Groups are open: anyone can register into any group, so invite codes go.
-- SQLite cannot drop a UNIQUE column, so the table is rebuilt. Migrations run
-- with foreign keys off (see store.Migrate), so dropping the old table does
-- not cascade into memberships.

CREATE TABLE groups_new (
    id            INTEGER PRIMARY KEY,
    code          TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    name          TEXT    NOT NULL,
    cist_group_id INTEGER,
    created_at    INTEGER NOT NULL
) STRICT;

INSERT INTO groups_new (id, code, name, cist_group_id, created_at)
    SELECT id, code, name, cist_group_id, created_at FROM groups;

DROP TABLE groups;
ALTER TABLE groups_new RENAME TO groups;
