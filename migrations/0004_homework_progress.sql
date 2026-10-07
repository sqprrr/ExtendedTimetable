-- M3: each student's own homework tracker. A row exists once the student
-- changes something; no row means "not started" and no grade. Rows are only
-- ever read or written by their own user (enforced in the service layer).

CREATE TABLE homework_progress (
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    homework_id INTEGER NOT NULL REFERENCES homework (id) ON DELETE CASCADE,
    status      TEXT    NOT NULL DEFAULT 'not_started'
                CHECK (status IN ('not_started', 'in_progress', 'done')),
    grade       REAL CHECK (grade IS NULL OR grade >= 0),
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, homework_id)
) STRICT;

CREATE INDEX homework_progress_homework_id ON homework_progress (homework_id);
