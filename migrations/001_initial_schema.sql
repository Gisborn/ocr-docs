-- +goose Up
-- +goose StatementBegin

-- Таблица организаций
CREATE TABLE organizations (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    email_verified BOOLEAN DEFAULT FALSE,
    password_hash VARCHAR(255), -- NULL для организаций, созданных через API
    tariff_id SMALLINT NOT NULL DEFAULT 1,
    balance_rub NUMERIC(10, 2) DEFAULT 0.00,
    prepaid_operations_left INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Таблица пользователей (доступ к личному кабинету)
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    is_admin BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(org_id, email)
);

-- Таблица API-ключей
CREATE TABLE api_keys (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    key_hash VARCHAR(255) NOT NULL, -- bcrypt hash
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    created_by INT REFERENCES users(id)
);

-- Таблица тарифов
CREATE TABLE tariffs (
    id SMALLINT PRIMARY KEY,
    code VARCHAR(50) UNIQUE NOT NULL, -- 'basic', 'enterprise'
    name VARCHAR(255) NOT NULL,
    type VARCHAR(20) NOT NULL CHECK (type IN ('on_demand', 'prepaid')),
    priority SMALLINT DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE
);

-- Таблица прайслистов тарифов
CREATE TABLE tariff_price_plans (
    id SERIAL PRIMARY KEY,
    tariff_id SMALLINT NOT NULL REFERENCES tariffs(id),
    document_type VARCHAR(50) NOT NULL DEFAULT 'passport_rf',  -- passport_rf, snils, inn, drivers_license и др.
    prepaid_qty INT DEFAULT 0,
    prepaid_price_rub NUMERIC(10, 2),
    on_demand_price_rub NUMERIC(10, 2) NOT NULL,
    UNIQUE(tariff_id, operation_type)
);

-- Таблица биллинг-транзакций (immutable)
CREATE TABLE billing_transactions (
    id BIGSERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id),
    type VARCHAR(20) NOT NULL CHECK (type IN ('reserve', 'commit', 'rollback')),
    status VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'COMMITTED', 'ROLLBACK')),
    amount_rub NUMERIC(10, 2) NOT NULL,
    operation_type VARCHAR(50) NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Таблица заказов (оплата через ЮКассу)
CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id),
    amount_rub NUMERIC(10, 2) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'PAID', 'FAILED')),
    payment_provider VARCHAR(50) DEFAULT 'yookassa',
    payment_id VARCHAR(255), -- ID платежа в ЮКассе
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    paid_at TIMESTAMPTZ
);

-- Таблица событий аккаунта (audit log)
CREATE TABLE account_events (
    id BIGSERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id),
    event_type VARCHAR(50) NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    actor_id INT REFERENCES users(id)
);

-- Индексы
CREATE INDEX idx_api_keys_org_id ON api_keys(org_id);
CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_billing_transactions_org_id ON billing_transactions(org_id);
CREATE INDEX idx_billing_transactions_status ON billing_transactions(status) WHERE status = 'PENDING';
CREATE INDEX idx_account_events_org_id ON account_events(org_id);
CREATE INDEX idx_account_events_type ON account_events(event_type);
CREATE INDEX idx_account_events_created_at ON account_events(created_at);

-- Insert базового тарифа
INSERT INTO tariffs (id, code, name, type, priority, is_active) 
VALUES (1, 'basic', 'Базовый', 'on_demand', 0, TRUE);

-- Insert прайслиста для базового тарифа
INSERT INTO tariff_price_plans (tariff_id, operation_type, prepaid_qty, on_demand_price_rub)
VALUES (1, 'passport_recognition', 0, 7.00);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS account_events CASCADE;
DROP TABLE IF EXISTS orders CASCADE;
DROP TABLE IF EXISTS billing_transactions CASCADE;
DROP TABLE IF EXISTS tariff_price_plans CASCADE;
DROP TABLE IF EXISTS tariffs CASCADE;
DROP TABLE IF EXISTS api_keys CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS organizations CASCADE;

-- +goose StatementEnd
