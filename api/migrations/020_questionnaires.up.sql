-- Анкета перед «Точкой перемен»: одна на человека (последние ответы).
-- Ссылку человек получает письмом после оплаты услуги или от Алексея
-- напрямую (кнопка в админке). Ответы – JSON {ключ вопроса: текст},
-- вопросы живут в handlers/anketa.go.
-- В ответах бывают сведения о здоровье (особая категория ПД, 152-ФЗ) –
-- без отдельного согласия анкета не принимается, момент согласия храним.
CREATE TABLE IF NOT EXISTS questionnaires (
  user_id           UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  answers           JSONB NOT NULL,
  health_consent_at TIMESTAMPTZ NOT NULL,
  submitted_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
