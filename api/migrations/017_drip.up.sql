-- Автоцепочка писем после регистрации на «Мягкий старт» (5 писем за 10 дней).
-- Одна строка = один шаг цепочки у одного человека: отправлено, пропущено
-- (например, человек уже купил продукт, о котором письмо) или не ушло.
CREATE TABLE IF NOT EXISTS drip_sends (
  user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  step     INT  NOT NULL CHECK (step BETWEEN 1 AND 20),
  status   TEXT NOT NULL CHECK (status IN ('sent','skipped','failed')),
  error    TEXT,
  sent_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, step)
);
CREATE INDEX IF NOT EXISTS drip_sends_sent_at_idx ON drip_sends(sent_at);

-- Цепочка идёт только тем, кто зарегистрировался ПОСЛЕ её запуска: старой
-- базе «день 1» не по смыслу. Момент запуска фиксируем один раз.
INSERT INTO site_settings(key, value) VALUES ('drip_start_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))
ON CONFLICT (key) DO NOTHING;
