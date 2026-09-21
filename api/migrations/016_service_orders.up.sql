-- Оплата услуг, а не только курсов.
--
-- «Точка перемен» – услуга: занятие и неделя сопровождения. Никакого курса
-- в базе для неё нет и быть не должно, поэтому заказ может ссылаться либо
-- на курс, либо на услугу.
ALTER TABLE orders ALTER COLUMN course_id DROP NOT NULL;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS service TEXT;

-- Ровно одно из двух: либо курс, либо услуга.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_course_or_service;
ALTER TABLE orders ADD CONSTRAINT orders_course_or_service
    CHECK ((course_id IS NOT NULL AND service IS NULL)
        OR (course_id IS NULL AND service IS NOT NULL));

CREATE INDEX IF NOT EXISTS idx_orders_service ON orders (service) WHERE service IS NOT NULL;
