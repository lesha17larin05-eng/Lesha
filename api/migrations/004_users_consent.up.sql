-- 152-ФЗ требует, чтобы оператор мог доказать факт получения согласия субъекта.
-- Поэтому в users добавляются две колонки с timestamp согласий:
--   consent_pd_at         — обязательное согласие на обработку ПД (NOT NULL после регистрации).
--   consent_marketing_at  — отдельное согласие на маркетинговые рассылки (NULL = не давал / отозвал).
-- Существующие пользователи: проставляем consent_pd_at = created_at как факт принятия
--                            по бывшим правилам (пометка). Маркетинг оставляем NULL.

ALTER TABLE users
    ADD COLUMN consent_pd_at        TIMESTAMPTZ,
    ADD COLUMN consent_marketing_at TIMESTAMPTZ;

UPDATE users SET consent_pd_at = created_at WHERE consent_pd_at IS NULL;
