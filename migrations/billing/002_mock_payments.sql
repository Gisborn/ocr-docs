-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS mock_payments (
    id SERIAL PRIMARY KEY,
    payment_id VARCHAR(64) UNIQUE NOT NULL,
    org_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL,
    amount_rub NUMERIC(12,2) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_mock_payments_org_id ON mock_payments(org_id);
CREATE INDEX idx_mock_payments_status ON mock_payments(status);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS mock_payments;

-- +goose StatementEnd
