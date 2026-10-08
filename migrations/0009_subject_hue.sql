-- subjects.hue is the colour a subject is shown in: one of eight hues
-- (blue, teal, green, amber, orange, rose, violet, slate), or '' for the
-- default, which is picked from the subject's id.
ALTER TABLE subjects ADD COLUMN hue TEXT NOT NULL DEFAULT '';
