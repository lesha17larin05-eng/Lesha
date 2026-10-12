-- Слежение за «зависшими» оплатами. Продамус возвращает покупателя на сайт
-- сразу после оплаты, а подтверждение (webhook) шлёт отдельно – и иногда
-- не шлёт вовсе (сбои на его стороне). returned_at – когда покупатель
-- вернулся с отметкой «оплачено»; stuck_alerted_at – когда Алексею ушло
-- уведомление, что подтверждения так и нет.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS returned_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS stuck_alerted_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_orders_stuck ON orders(returned_at) WHERE status = 'pending' AND stuck_alerted_at IS NULL;
