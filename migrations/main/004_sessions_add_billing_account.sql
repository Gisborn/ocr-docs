-- +goose Up
-- +goose StatementBegin

ALTER TABLE sessions ADD COLUMN billing_account_id BIGINT DEFAULT 0;

UPDATE sessions s
SET billing_account_id = COALESCE(o.billing_account_id, 0)
FROM organizations o
WHERE s.org_id = o.id;

-- +goose StatementEnd
