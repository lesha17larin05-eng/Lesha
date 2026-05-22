ALTER TABLE users
    DROP COLUMN IF EXISTS consent_pd_at,
    DROP COLUMN IF EXISTS consent_marketing_at;
