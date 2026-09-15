-- Простое key-value хранилище настроек сайта, которые Алексей переключает
-- из админки без деплоя. Значения — строки ('true'/'false' для флагов),
-- белый список ключей и дефолты живут в api/internal/db/queries.go.
CREATE TABLE IF NOT EXISTS site_settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Страница «Здоровая спина в Салюте» (/salut-2026) вне летнего сезона скрыта:
-- кнопки в шапке нет, в sitemap не попадает, отдаётся noindex.
-- Сама страница остаётся доступной по прямой ссылке.
INSERT INTO site_settings(key, value) VALUES ('salut_visible', 'false')
ON CONFLICT (key) DO NOTHING;
