-- users.locale now holds the language a user chose, or '' when they have not
-- chosen one and see the language of their browser's cookie or the site
-- default. Before M5 nobody could choose, so every 'uk' is the old default.
UPDATE users SET locale = '' WHERE locale = 'uk';
