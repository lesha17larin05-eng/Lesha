-- Откуда пришёл человек: метка из ссылки (?from=insta, ?utm_source=...) или
-- догадка по сайту-источнику (ref:instagram). Пишется один раз – при
-- регистрации (users) и при создании заказа (orders).
ALTER TABLE users  ADD COLUMN IF NOT EXISTS source TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS source TEXT;
CREATE INDEX IF NOT EXISTS idx_users_source  ON users(source)  WHERE source IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orders_source ON orders(source) WHERE source IS NOT NULL;
