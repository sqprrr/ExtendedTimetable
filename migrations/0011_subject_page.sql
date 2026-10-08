-- The subject page: who teaches the subject and its distance-learning page,
-- filled in by the leader ('' when not set), and the lesson type of a
-- recording or solution (NULL when it is not tied to one).

ALTER TABLE subjects ADD COLUMN lecturer   TEXT NOT NULL DEFAULT '';
ALTER TABLE subjects ADD COLUMN instructor TEXT NOT NULL DEFAULT '';
ALTER TABLE subjects ADD COLUMN dl_url     TEXT NOT NULL DEFAULT '';

ALTER TABLE resource_links ADD COLUMN lesson_type TEXT
    CHECK (lesson_type IS NULL OR lesson_type IN ('lecture', 'practice', 'lab'));
