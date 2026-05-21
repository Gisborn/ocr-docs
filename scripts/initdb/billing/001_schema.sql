-- +goose StatementBegin

-- ENUM типы
CREATE TYPE billing_event_type AS ENUM (
    'account_created',
    'balance_topup',
    'subscription_charge',
    'upgrade_payment',
    'upgrade_bonus',
    'prepaid_usage',
    'pay_as_you_go',
    'subscription_expired',
    'refund'
);

CREATE TYPE subscription_status AS ENUM (
    'active', 
    'grace_period', 
    'expired', 
    'pending_downgrade', 
    'upgraded', 
    'cancelled'
);

-- Таблица счетов клиентов
CREATE TABLE accounts (
    id BIGSERIAL PRIMARY KEY,
    status VARCHAR(20) NOT NULL DEFAULT 'active' 
        CHECK (status IN ('active', 'blocked', 'archived')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Таблица услуг
CREATE TABLE services (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'deprecated', 'archived')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Справочник тарифов
CREATE TABLE tariffs (
    id SMALLSERIAL PRIMARY KEY,
    code VARCHAR(50) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Версии тарифов
CREATE TABLE tariff_versions (
    id SERIAL PRIMARY KEY,
    tariff_id SMALLINT REFERENCES tariffs(id),
    valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until TIMESTAMPTZ,
    duration_days INTEGER NOT NULL DEFAULT 30,
    base_price_rub NUMERIC(10, 2) NOT NULL,
    prepaid_amount_rub NUMERIC(10, 2) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (tariff_id, valid_from)
);

-- Цены операций по версиям тарифов
CREATE TABLE tariff_service_prices (
    id SERIAL PRIMARY KEY,
    tariff_version_id INTEGER REFERENCES tariff_versions(id),
    service_id VARCHAR(50) REFERENCES services(id),
    included_price_rub NUMERIC(10, 2) NOT NULL,
    overage_price_rub NUMERIC(10, 2) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (tariff_version_id, service_id)
);

-- Подписки
CREATE TABLE subscriptions (
    id SERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    tariff_version_id INTEGER REFERENCES tariff_versions(id),
    status subscription_status DEFAULT 'active',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    grace_period_ends_at TIMESTAMPTZ,
    initial_prepaid_rub NUMERIC(10, 2) NOT NULL,
    auto_renew BOOLEAN DEFAULT FALSE,
    next_tariff_version_id INTEGER REFERENCES tariff_versions(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Event Sourcing - основная таблица событий
CREATE TABLE billing_events (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    subscription_id INTEGER REFERENCES subscriptions(id),
    service_id VARCHAR(50) REFERENCES services(id),
    type billing_event_type NOT NULL,
    real_amount_rub NUMERIC(10, 2) DEFAULT 0.00,
    prepaid_amount_rub NUMERIC(10, 2) DEFAULT 0.00,
    request_id VARCHAR(100) UNIQUE,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Снапшоты балансов (кэш для чтения)
CREATE TABLE balance_snapshots (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id),
    real_balance_rub NUMERIC(10, 2) DEFAULT 0.00,
    prepaid_balance_rub NUMERIC(10, 2) DEFAULT 0.00,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Резервы (PENDING транзакции)
CREATE TABLE reservations (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    subscription_id INTEGER REFERENCES subscriptions(id),
    service_id VARCHAR(50) REFERENCES services(id),
    request_id VARCHAR(100) UNIQUE NOT NULL,
    amount_rub NUMERIC(10, 2) NOT NULL,
    charge_type VARCHAR(20) NOT NULL CHECK (charge_type IN ('prepaid', 'pay_as_you_go')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

-- История изменений подписок
CREATE TABLE subscription_changes (
    id SERIAL PRIMARY KEY,
    subscription_id INTEGER REFERENCES subscriptions(id),
    change_type VARCHAR(20) CHECK (change_type IN ('upgrade', 'downgrade', 'renewal')),
    from_tariff_version_id INTEGER REFERENCES tariff_versions(id),
    to_tariff_version_id INTEGER REFERENCES tariff_versions(id),
    prorated_amount_rub NUMERIC(10, 2),
    payment_method VARCHAR(20),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Заказы на оплату (ЮКасса)
CREATE TABLE payment_orders (
    id SERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    amount_rub NUMERIC(10, 2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'paid', 'failed', 'cancelled')),
    yookassa_payment_id VARCHAR(255),
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    paid_at TIMESTAMPTZ
);

-- Индексы
CREATE INDEX idx_tariff_versions_validity ON tariff_versions(tariff_id, valid_from, valid_until);
CREATE INDEX idx_tariff_prices_version ON tariff_service_prices(tariff_version_id, service_id);
CREATE INDEX idx_subscriptions_account_active ON subscriptions(account_id, status) 
    WHERE status IN ('active', 'grace_period');
CREATE INDEX idx_billing_events_account_time ON billing_events(account_id, created_at);
CREATE INDEX idx_billing_events_request_id ON billing_events(request_id);
CREATE INDEX idx_reservations_account ON reservations(account_id);
CREATE INDEX idx_reservations_expires ON reservations(expires_at);
CREATE INDEX idx_payment_orders_account ON payment_orders(account_id);
CREATE INDEX idx_payment_orders_yookassa ON payment_orders(yookassa_payment_id);

-- Триггер для UUID generation billing_events
CREATE OR REPLACE FUNCTION generate_billing_event_uuid()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.request_id IS NULL THEN
        NEW.request_id = gen_random_uuid()::text;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_request_id
    BEFORE INSERT ON billing_events
    FOR EACH ROW EXECUTE FUNCTION generate_billing_event_uuid();

-- Триггеры для updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_accounts_updated_at BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_services_updated_at BEFORE UPDATE ON services
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_tariffs_updated_at BEFORE UPDATE ON tariffs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_subscriptions_updated_at BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Начальные данные
INSERT INTO services (id, name, description, status) 
VALUES ('passport_rf', 'Паспорт РФ', 'Распознавание паспорта гражданина РФ', 'active');

-- Тариф Free
INSERT INTO tariffs (id, code, name, description, is_active) 
VALUES (1, 'free', 'Free', 'Бесплатный тариф без подписки', TRUE);

INSERT INTO tariff_versions (tariff_id, valid_from, valid_until, duration_days, base_price_rub, prepaid_amount_rub)
VALUES (1, '2024-01-01', NULL, 30, 0.00, 0.00);

-- Тариф Basic (pay-as-you-go)
INSERT INTO tariffs (id, code, name, description, is_active) 
VALUES (2, 'basic', 'Basic', 'Оплата по факту использования', TRUE);

INSERT INTO tariff_versions (tariff_id, valid_from, valid_until, duration_days, base_price_rub, prepaid_amount_rub)
VALUES (2, '2024-01-01', NULL, 30, 0.00, 0.00);

-- Тариф Pro (prepaid)
INSERT INTO tariffs (id, code, name, description, is_active) 
VALUES (3, 'pro', 'Pro', 'Предоплаченный пакет операций', TRUE);

INSERT INTO tariff_versions (tariff_id, valid_from, valid_until, duration_days, base_price_rub, prepaid_amount_rub)
VALUES (3, '2024-01-01', NULL, 30, 20000.00, 6000.00);

-- Цены для Basic (только overage)
INSERT INTO tariff_service_prices (tariff_version_id, service_id, included_price_rub, overage_price_rub)
VALUES (2, 'passport_rf', 7.00, 7.00);

-- Цены для Pro (included + overage)
INSERT INTO tariff_service_prices (tariff_version_id, service_id, included_price_rub, overage_price_rub)
VALUES (3, 'passport_rf', 1.00, 5.00);

-- +goose StatementEnd

