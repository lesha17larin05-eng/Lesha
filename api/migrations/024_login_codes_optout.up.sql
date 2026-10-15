-- 1) Вход по коду из письма и по ссылке из письма о доступе.
--
-- login_codes – 6-значный код на почту (живёт 15 минут, 5 попыток).
-- Храним только хеш: код короткий, поэтому хеш с солью из секрета (HMAC) –
-- считается в коде, здесь просто строка.
CREATE TABLE IF NOT EXISTS login_codes (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash   TEXT NOT NULL,
  attempts    INT  NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at  TIMESTAMPTZ NOT NULL,
  used_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_login_codes_user ON login_codes(user_id, created_at DESC);

-- login_links – кнопка «Перейти к урокам» в письме о доступе: входит без
-- пароля и ведёт к курсу. Многоразовая до истечения срока (7 дней): человек
-- открывает письмо и на телефоне, и на компьютере.
CREATE TABLE IF NOT EXISTS login_links (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash   TEXT NOT NULL UNIQUE,
  next_path    TEXT NOT NULL DEFAULT '/cabinet',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at   TIMESTAMPTZ NOT NULL,
  last_used_at TIMESTAMPTZ,
  uses         INT NOT NULL DEFAULT 0
);

-- 2) Отказ от писем. Раньше отписка просто стирала consent_marketing_at,
-- и «не давал согласия» было не отличить от «отписался». Теперь отписке
-- соответствует отметка unsubscribed_at: таким людям не шлём ни писем
-- цепочки, ни предложений подписаться.
ALTER TABLE users ADD COLUMN IF NOT EXISTS unsubscribed_at TIMESTAMPTZ;

-- Кто уже отписался до этой миграции: согласия нет, а письма с согласием
-- (цепочка или рассылка подписанным) им уходили, или оставлена причина отписки.
UPDATE users u SET unsubscribed_at = now()
 WHERE u.consent_marketing_at IS NULL AND u.unsubscribed_at IS NULL
   AND (EXISTS (SELECT 1 FROM drip_sends d WHERE d.user_id = u.id AND d.status = 'sent')
        OR EXISTS (SELECT 1 FROM unsubscribe_reasons x WHERE x.user_id = u.id)
        OR EXISTS (SELECT 1 FROM campaign_recipients cr JOIN campaigns c ON c.id = cr.campaign_id
                    WHERE cr.user_id = u.id AND cr.status = 'sent' AND c.segment <> 'service_paid'));

-- 3) Письма 1–3 цепочки «Мягкого старта» теперь уходят и тем, кто не ставил
-- галочку рассылки (это подсказки по курсу, без предложений). Только тем, кто
-- зарегистрировался после этой миграции: в первом письме «вчера вы взяли
-- курс», старым регистрациям оно не подходит – им уходит разовое письмо.
INSERT INTO site_settings(key, value)
VALUES ('drip_nosub_start_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))
ON CONFLICT (key) DO NOTHING;
