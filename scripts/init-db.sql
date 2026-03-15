-- Инициализация БД для локальной разработки
-- Создаем тестовую организацию и API-ключ для разработки

-- Тестовая организация (password: 'test123')
INSERT INTO organizations (id, name, email, email_verified, password_hash, tariff_id, balance_rub)
VALUES (
    1, 
    'Test Organization', 
    'test@example.com', 
    true, 
    '$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi', -- bcrypt hash for 'password'
    1, 
    1000.00
) ON CONFLICT (id) DO NOTHING;

-- Тестовый API-ключ (key: 'test-api-key-12345')
INSERT INTO api_keys (id, org_id, name, key_hash, created_at)
VALUES (
    1,
    1,
    'Development Key',
    '$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi', -- bcrypt hash for 'test-api-key-12345'
    NOW()
) ON CONFLICT (id) DO NOTHING;
