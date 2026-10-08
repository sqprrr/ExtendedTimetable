-- Editors: members the leader trusts to change the group's content. They do
-- everything the leader does except handle the invite link and members.
-- SQLite cannot change a CHECK constraint, so memberships and group_log are
-- rebuilt. Migrations run with foreign keys off (see store.Migrate).

CREATE TABLE memberships_new (
    user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    group_id  INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    role      TEXT    NOT NULL CHECK (role IN ('student', 'editor', 'leader')),
    joined_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, group_id)
) STRICT;

INSERT INTO memberships_new (user_id, group_id, role, joined_at)
    SELECT user_id, group_id, role, joined_at FROM memberships;

DROP TABLE memberships;
ALTER TABLE memberships_new RENAME TO memberships;

CREATE INDEX memberships_group_id ON memberships (group_id);
CREATE UNIQUE INDEX memberships_one_leader ON memberships (group_id) WHERE role = 'leader';

CREATE TABLE group_log_new (
    id         INTEGER PRIMARY KEY,
    group_id   INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    event      TEXT    NOT NULL CHECK (event IN (
                   'group_created', 'joined', 'left', 'removed',
                   'leader_claimed', 'leader_resigned', 'leader_assigned', 'leader_removed',
                   'editor_granted', 'editor_revoked',
                   'invite_regenerated')),
    actor_id   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    user_id    INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL
) STRICT;

INSERT INTO group_log_new (id, group_id, event, actor_id, user_id, created_at)
    SELECT id, group_id, event, actor_id, user_id, created_at FROM group_log;

DROP TABLE group_log;
ALTER TABLE group_log_new RENAME TO group_log;

CREATE INDEX group_log_group_created ON group_log (group_id, created_at);
