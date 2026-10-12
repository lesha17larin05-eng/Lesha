-- Повторная отправка писем, которые не ушли (почта лежала).
-- email_outbox – разовые письма (доступ к курсу, оплата, заявки): воркер
-- пробует снова с растущей паузой, до 3 суток.
CREATE TABLE IF NOT EXISTS email_outbox (
  id          BIGSERIAL PRIMARY KEY,
  to_addr     TEXT NOT NULL,
  subject     TEXT NOT NULL,
  body        TEXT NOT NULL,
  attempts    INT  NOT NULL DEFAULT 1,
  last_error  TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  next_at     TIMESTAMPTZ NOT NULL,
  sent_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS email_outbox_due_idx ON email_outbox(next_at) WHERE sent_at IS NULL;

-- Цепочка: шаг со статусом failed пробуем снова (не чаще раза в 30 минут,
-- не больше 8 попыток).
ALTER TABLE drip_sends ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 1;
