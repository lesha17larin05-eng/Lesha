-- Счётчик чтения статей. Нужен, чтобы в админке было видно, какие статьи
-- живые, а какие пора переписать или снять.
--
-- Персональных данных не храним сознательно: ни IP, ни user-agent, ни
-- идентификатора посетителя. Только «на такой-то статье случилось такое-то
-- событие в такое-то время» – по 152-ФЗ это не персональные данные и
-- согласия не требует.
--
-- Три события:
--   open — страницу открыли
--   read — дочитали (долистали до конца текста)
--   cta  — нажали ссылку на платную страницу из статьи
CREATE TABLE IF NOT EXISTS article_views (
    id          BIGSERIAL PRIMARY KEY,
    article_id  UUID NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    event       TEXT NOT NULL CHECK (event IN ('open', 'read', 'cta')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS article_views_article_idx ON article_views (article_id, event);
CREATE INDEX IF NOT EXISTS article_views_created_idx ON article_views (created_at DESC);
