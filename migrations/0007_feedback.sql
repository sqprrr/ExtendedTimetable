-- Feedback: bug reports, suggestions and reviews that signed-in users send to
-- the site's superadmins. A review may carry a 1–5 rating. Feedback outlives
-- its author's account (user_id becomes NULL).

CREATE TABLE feedback (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('bug', 'idea', 'review')),
    rating      INTEGER CHECK (rating IS NULL OR (kind = 'review' AND rating BETWEEN 1 AND 5)),
    message     TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    -- resolved_at is set once a superadmin has dealt with it.
    resolved_at INTEGER
) STRICT;

CREATE INDEX feedback_user_id ON feedback (user_id);
