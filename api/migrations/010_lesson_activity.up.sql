-- Журнал просмотров уроков: одна строка = одна «сессия» просмотра.
-- В отличие от lesson_progress (одна строка на пару user+lesson, последнее
-- состояние), здесь повторные просмотры видны отдельными записями.
-- Сессия «склеивается», пока между касаниями < 30 минут (логика в repo).
CREATE TABLE IF NOT EXISTS lesson_activity (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  lesson_id UUID NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  max_position_sec INTEGER NOT NULL DEFAULT 0,
  completed BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_lesson_activity_updated ON lesson_activity(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_lesson_activity_user_lesson ON lesson_activity(user_id, lesson_id, updated_at DESC);

-- Перенос уже накопленного состояния из lesson_progress как стартовых сессий,
-- чтобы журнал не начинался с пустоты.
INSERT INTO lesson_activity(user_id, lesson_id, started_at, updated_at, max_position_sec, completed)
SELECT user_id, lesson_id, updated_at, updated_at, last_position_sec, completed_at IS NOT NULL
  FROM lesson_progress;
