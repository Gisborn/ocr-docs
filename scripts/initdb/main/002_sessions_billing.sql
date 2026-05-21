-- Добавляем billing_account_id в sessions
ALTER TABLE sessions ADD COLUMN billing_account_id BIGINT DEFAULT 0;

-- Обновляем существующие сессии из organizations
UPDATE sessions s
SET billing_account_id = COALESCE(o.billing_account_id, 0)
FROM organizations o
WHERE s.org_id = o.id;
