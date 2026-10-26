-- «Люди» – простая база контактов поверх заявок (leads).
-- Записи добавляются с форм сайта, вручную из админки и при оплате услуг.
-- У каждого – интерес, заметка и следующий шаг с датой (задачи на неделю).
ALTER TABLE leads ADD COLUMN IF NOT EXISTS interest  TEXT NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS note      TEXT NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS next_step TEXT NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS next_at   DATE;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Статусы: new – новый, in_progress – в разговоре, bought – купил,
-- later – не сейчас, no – не подходит; done – прежнее «обработана».
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_status_check;
ALTER TABLE leads ADD CONSTRAINT leads_status_check
  CHECK (status IN ('new','in_progress','bought','later','no','done'));

-- Интерес заявок с сайта = страница, с которой они пришли.
UPDATE leads SET interest = source
 WHERE interest = '' AND source IN ('coaching','gruppa','personal','start');

CREATE INDEX IF NOT EXISTS idx_leads_next_at ON leads(next_at) WHERE next_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_leads_contact_lower ON leads(lower(contact));
