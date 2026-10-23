-- Анкета бывает разной: перед первым занятием («start») и перед групповым
-- ведением («gruppa»). Храним, какую анкету человек заполнил, – по ней
-- админка и письмо показывают правильные вопросы.
ALTER TABLE questionnaires ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'start';
