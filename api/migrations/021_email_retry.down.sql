ALTER TABLE drip_sends DROP COLUMN IF EXISTS attempts;
DROP TABLE IF EXISTS email_outbox;
