-- Напоминание про анкету группы: через 2 дня после оплаты, если анкета
-- не заполнена, – одно письмо. Отметка, чтобы не слать повторно.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS anketa_reminded_at TIMESTAMPTZ;
