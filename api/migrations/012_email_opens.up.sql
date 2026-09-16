-- Открытия писем рассылки: одна строка = одно срабатывание пикселя.
-- Нужна, чтобы отличать «письмо прочитали и не заинтересовались»
-- от «письмо не дошло / лежит в спаме».
-- IP не храним намеренно: для статистики хватает пользователя и времени.
CREATE TABLE IF NOT EXISTS email_opens (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  campaign   TEXT NOT NULL,
  opened_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_agent TEXT
);

CREATE INDEX IF NOT EXISTS email_opens_campaign_idx ON email_opens(campaign, opened_at DESC);
CREATE INDEX IF NOT EXISTS email_opens_user_idx ON email_opens(user_id);
