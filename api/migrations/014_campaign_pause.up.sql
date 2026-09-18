-- Интервал между письмами внутри одной рассылки, в секундах.
-- Раньше был жёстко зашит (45 секунд), теперь задаётся при создании.
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS pause_sec INT NOT NULL DEFAULT 120;
