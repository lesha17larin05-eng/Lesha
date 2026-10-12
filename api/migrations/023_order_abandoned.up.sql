-- Брошенная оплата: человек перешёл к оплате, но так и не оплатил.
-- abandoned_alerted_at – когда Алексею ушло уведомление (один раз на заказ).
ALTER TABLE orders ADD COLUMN IF NOT EXISTS abandoned_alerted_at TIMESTAMPTZ;
