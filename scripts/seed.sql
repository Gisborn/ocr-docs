-- Seed data for local testing
-- Run this after migrations to populate test data

-- ============================================
-- BILLING DB: Tariffs and Services
-- ============================================

-- Insert services
INSERT INTO services (name, description, status) VALUES 
    ('passport_rf', 'Паспорт РФ (распознавание)', 'active')
ON CONFLICT DO NOTHING;

-- Insert tariffs
INSERT INTO tariffs (code, name, description) VALUES 
    ('free', 'Free', 'Бесплатный тариф с предоплаченным пакетом'),
    ('basic', 'Basic', 'Pay-as-you-go тариф'),
    ('pro', 'Pro', 'Подписка с prepaid пакетом')
ON CONFLICT DO NOTHING;

-- Insert tariff versions
INSERT INTO tariff_versions (tariff_id, duration_days, base_price_rub, prepaid_amount_rub, valid_from, valid_until) VALUES 
    ((SELECT id FROM tariffs WHERE code = 'free'), 30, 0, 1000, NOW() - INTERVAL '1 year', NULL),
    ((SELECT id FROM tariffs WHERE code = 'basic'), 30, 0, 0, NOW() - INTERVAL '1 year', NULL),
    ((SELECT id FROM tariffs WHERE code = 'pro'), 30, 20000, 6000, NOW() - INTERVAL '1 year', NULL)
ON CONFLICT DO NOTHING;

-- Insert service prices for pro tariff
INSERT INTO tariff_service_prices (tariff_version_id, service_id, included_price_rub, overage_price_rub) VALUES 
    ((SELECT tv.id FROM tariff_versions tv JOIN tariffs t ON tv.tariff_id = t.id WHERE t.code = 'free'),
     (SELECT id FROM services WHERE name = 'passport_rf'),
     0, 7),
    ((SELECT tv.id FROM tariff_versions tv JOIN tariffs t ON tv.tariff_id = t.id WHERE t.code = 'basic'),
     (SELECT id FROM services WHERE name = 'passport_rf'),
     0, 7),
    ((SELECT tv.id FROM tariff_versions tv JOIN tariffs t ON tv.tariff_id = t.id WHERE t.code = 'pro'),
     (SELECT id FROM services WHERE name = 'passport_rf'),
     0, 7)
ON CONFLICT DO NOTHING;

-- ============================================
-- MAIN DB: Test Organization and User
-- ============================================

-- Password hash for 'test123' (bcrypt)
-- $2a$10$YourHashHere...

-- Create test organization (if not exists)
DO $$
DECLARE
    org_id bigint;
    user_id bigint;
    billing_account_id bigint := 1;
BEGIN
    -- Check if test org already exists
    SELECT id INTO org_id FROM organizations WHERE email = 'test@example.com';
    
    IF org_id IS NULL THEN
        -- Create organization
        INSERT INTO organizations (name, email, email_verified, password_hash, billing_account_id, status)
        VALUES (
            'Test Company',
            'test@example.com',
            true,
            '$2a$10$kIxY6tX2MRiV4tROQZHKOenezw37Hdc1s14qDCSy9jsqBYFDP2Xde', -- password: 'password'
            billing_account_id,
            'active'
        )
        RETURNING id INTO org_id;
        
        -- Create admin user
        INSERT INTO users (org_id, email, password_hash, role)
        VALUES (
            org_id,
            'test@example.com',
            '$2a$10$kIxY6tX2MRiV4tROQZHKOenezw37Hdc1s14qDCSy9jsqBYFDP2Xde', -- password: 'password'
            'admin'
        )
        RETURNING id INTO user_id;
        
        -- Add event
        INSERT INTO account_events (org_id, event_type, payload, actor_id)
        VALUES (
            org_id,
            'organization_registered',
            '{"email": "test@example.com", "source": "seed"}',
            user_id
        );
        
        RAISE NOTICE 'Created test organization with ID: % and user ID: %', org_id, user_id;
    ELSE
        RAISE NOTICE 'Test organization already exists with ID: %', org_id;
    END IF;
END $$;
