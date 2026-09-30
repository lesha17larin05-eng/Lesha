-- Анкета перед «Точкой перемен»: одна на оплаченный заказ услуги.
-- Ответы – JSON {ключ вопроса: текст}; вопросы живут в handlers/anketa.go.
-- В ответах бывают сведения о здоровье (особая категория ПД, 152-ФЗ) –
-- без отдельного согласия анкета не принимается, момент согласия храним.
CREATE TABLE IF NOT EXISTS questionnaires (
  order_id          UUID PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
  user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  answers           JSONB NOT NULL,
  health_consent_at TIMESTAMPTZ NOT NULL,
  submitted_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS questionnaires_user_idx ON questionnaires(user_id);
