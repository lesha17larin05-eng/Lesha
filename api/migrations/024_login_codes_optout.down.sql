DELETE FROM site_settings WHERE key = 'drip_nosub_start_at';
ALTER TABLE users DROP COLUMN IF EXISTS unsubscribed_at;
DROP TABLE IF EXISTS login_links;
DROP TABLE IF EXISTS login_codes;
