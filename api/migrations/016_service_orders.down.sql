DROP INDEX IF EXISTS idx_orders_service;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_course_or_service;
DELETE FROM orders WHERE course_id IS NULL;
ALTER TABLE orders DROP COLUMN IF EXISTS service;
ALTER TABLE orders ALTER COLUMN course_id SET NOT NULL;
