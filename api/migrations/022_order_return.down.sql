DROP INDEX IF EXISTS idx_orders_stuck;
ALTER TABLE orders DROP COLUMN IF EXISTS stuck_alerted_at;
ALTER TABLE orders DROP COLUMN IF EXISTS returned_at;
