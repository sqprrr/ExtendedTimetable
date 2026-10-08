-- users.theme holds the colour theme a user chose: 'system' (follow the
-- device), 'light' or 'dark'; '' when they have not chosen and see the theme
-- cookie's choice or the device's.
ALTER TABLE users ADD COLUMN theme TEXT NOT NULL DEFAULT '';
