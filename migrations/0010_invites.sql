-- Invite links, one leader per group and the group log.
--
-- Groups are no longer open: a group is joined only through its invite link,
-- /join/<token>. Each group has one link at a time; it stops working when it
-- expires or is replaced. Existing groups get a fresh link here.

CREATE TABLE group_invites (
    group_id   INTEGER PRIMARY KEY REFERENCES groups (id) ON DELETE CASCADE,
    token      TEXT    NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

INSERT INTO group_invites (group_id, token, expires_at, created_at)
    SELECT id, lower(hex(randomblob(32))), unixepoch() + 10 * 86400, unixepoch() FROM groups;

-- A group has at most one leader. Where the CLI made several, the one who
-- joined first stays leader.
UPDATE memberships SET role = 'student'
WHERE role = 'leader' AND EXISTS (
    SELECT 1 FROM memberships m
    WHERE m.group_id = memberships.group_id AND m.role = 'leader'
      AND (m.joined_at < memberships.joined_at
           OR (m.joined_at = memberships.joined_at AND m.user_id < memberships.user_id))
);

CREATE UNIQUE INDEX memberships_one_leader ON memberships (group_id) WHERE role = 'leader';

-- What happened to a group's membership, for the superadmins. actor_id is who
-- did it (NULL: the server CLI); user_id is whom it was done to.
CREATE TABLE group_log (
    id         INTEGER PRIMARY KEY,
    group_id   INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    event      TEXT    NOT NULL CHECK (event IN (
                   'group_created', 'joined', 'left', 'removed',
                   'leader_claimed', 'leader_resigned', 'leader_assigned', 'leader_removed',
                   'invite_regenerated')),
    actor_id   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    user_id    INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX group_log_group_created ON group_log (group_id, created_at);
