-- Рассылки из админки: письмо + список получателей, зафиксированный
-- на момент запуска. Отправка идёт фоном, по одному письму с паузой,
-- не больше daily_limit в сутки.
CREATE TABLE IF NOT EXISTS campaigns (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name        TEXT NOT NULL,
  subject     TEXT NOT NULL,
  body        TEXT NOT NULL,                 -- простой текст, абзацы через пустую строку
  segment     TEXT NOT NULL,                 -- ключ группы (см. db.Segments)
  daily_limit INT  NOT NULL DEFAULT 50,
  status      TEXT NOT NULL DEFAULT 'draft'
              CHECK (status IN ('draft','sending','paused','done')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at  TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);

-- Получатели фиксируются при создании рассылки: если человек отпишется
-- уже после старта, отправщик его пропустит (проверяет согласие перед
-- каждым письмом), но список останется для отчётности.
CREATE TABLE IF NOT EXISTS campaign_recipients (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email       TEXT NOT NULL,
  name        TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'pending'
              CHECK (status IN ('pending','sent','failed','skipped')),
  sent_at     TIMESTAMPTZ,
  error       TEXT,
  UNIQUE (campaign_id, user_id)
);

CREATE INDEX IF NOT EXISTS campaign_recipients_queue_idx ON campaign_recipients(campaign_id, status);
CREATE INDEX IF NOT EXISTS campaign_recipients_sent_idx ON campaign_recipients(sent_at);
