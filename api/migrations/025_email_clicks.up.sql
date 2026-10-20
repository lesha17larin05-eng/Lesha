-- Переходы по ссылкам из писем (цепочка «Мягкого старта» и рассылки).
-- Ссылки в письме ведут на /api/c (подписанный редирект): переход
-- записывается сюда, человек сразу попадает на нужную страницу.
-- campaign – как в email_opens: 'drip-<шаг>' или id рассылки.
CREATE TABLE IF NOT EXISTS email_clicks (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  campaign   TEXT NOT NULL,
  url        TEXT NOT NULL,
  user_agent TEXT,
  clicked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS email_clicks_campaign_idx ON email_clicks(campaign, clicked_at DESC);
CREATE INDEX IF NOT EXISTS email_clicks_user_idx ON email_clicks(user_id);
