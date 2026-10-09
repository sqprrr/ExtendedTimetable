-- users.time_zone holds the time zone a user chose for dates: an IANA name
-- ('Europe/Warsaw') or 'auto' (the device's); '' when they have not chosen
-- and see the zone cookie's choice or the device's.
ALTER TABLE users ADD COLUMN time_zone TEXT NOT NULL DEFAULT '';
