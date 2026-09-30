-- Причины отписки: человек выбирает на странице /unsubscribe перед
-- подтверждением. Одна строка = одна отписка (повторные тоже пишутся).
CREATE TABLE IF NOT EXISTS unsubscribe_reasons (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason     TEXT NOT NULL,
  comment    TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS unsubscribe_reasons_created_idx ON unsubscribe_reasons(created_at DESC);
