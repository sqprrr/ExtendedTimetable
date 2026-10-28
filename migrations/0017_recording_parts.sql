-- A class recorded in several videos ("КЕ ЛБ 1 частина 2"): part is the
-- part the title or description names, 0 if none. The parts of a class
-- share its number ("Лабораторна №1, частина 2").
ALTER TABLE youtube_videos ADD COLUMN part INTEGER NOT NULL DEFAULT 0;
