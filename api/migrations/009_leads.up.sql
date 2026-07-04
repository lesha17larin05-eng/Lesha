-- Заявки с маркетинговых страниц (/coaching, /consultation).
-- Персональные данные: только soft-обработка, записи не удаляем (152-ФЗ — согласие фиксируем).
CREATE TABLE IF NOT EXISTS leads (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  contact TEXT NOT NULL,               -- email / телефон / @telegram — как удобно человеку
  message TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',     -- coaching | consultation | ...
  status TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new','in_progress','done')),
  consent_pd BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_leads_created_at ON leads(created_at DESC);
