-- Скрываем 6 слабых статей (тексты остаются в базе, можно вернуть).
UPDATE articles SET is_published = false WHERE slug IN ('muzhskoe-zhenskoe-zdorove','banja-i-zdorove','sotsialnoe-zdorove','gimnastika-dlja-glaz','obuchenie-novomu-navyku','akrojoga-chto-eto');
