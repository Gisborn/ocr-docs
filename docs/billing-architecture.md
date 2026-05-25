# Архитектура Billing Service

## Обзор

Billing Service — **отдельный микросервис** с собственной базой данных. Управляет:
- Счетами (`accounts`)
- Подписками и тарифами
- Платежами (двойная система: prepaid + pay-as-you-go)

## Принципы

1. **Event Sourcing** — все операции как immutable события
2. **Отдельная БД** — полная автономность от других сервисов
3. **Высокая нагрузка** — при 100 RPS только INSERT
4. **Только REST API** — HTTP для всех взаимодействий (внутренних и внешних)

---

## Границы сервиса

### Что владеет Billing Service

- `accounts` — счета клиентов (без email!)
- `tariffs`, `services`, `tariff_service_prices`
- `subscriptions` — подписки
- `billing_events` — события (Event Sourcing)
- `balance_snapshots` — кэш балансов
- `subscription_changes` — audit

### Взаимодействие с другими сервисами

```
Cabinet Service          Billing Service          Orchestrator
      |                        |                        |
      |-- CreateAccount ------>|                        |
      |   (HTTP/REST)          |                        |
      |                        |                        |
      |-- GetBalance --------->|                        |
      |                        |                        |
      |                        |<-- Reserve() ----------|
      |                        |   (HTTP/REST)          |
      |                        |                        |
      |                        |<-- Commit/Rollback()--|
```

**Важно:** Billing Service **не знает** о пользователях, API-ключах, событиях аккаунта. Только `account_id`.

---

## Схема данных (отдельная БД `billing_db`)

### 1. `accounts` — Счета клиентов

```sql
CREATE TABLE accounts (
    id BIGSERIAL PRIMARY KEY,        -- ID генерирует Billing Service
    status VARCHAR(20) NOT NULL DEFAULT 'active' 
        CHECK (status IN ('active', 'blocked', 'archived')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
    -- Нет name, external_id! Billing не знает о клиентах.
    -- Cabinet хранит маппинг org_id -> billing_account_id у себя.
);
```

**Статусы `accounts`:**
- `active` — нормальная работа, списания разрешены
- `blocked` — заблокирован (неплатёж, нарушение), списания запрещены, API возвращает 402
- `archived` — удалён клиентом, данные сохранены для аудита, любые операции запрещены (404)

### 2. `services` — Типы операций

```sql
CREATE TABLE services (
    id VARCHAR(50) PRIMARY KEY,      -- passport_rf, snils, inn...
    name VARCHAR(255) NOT NULL,
    description TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'deprecated', 'archived')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

**Статусы `services`:**
- `active` — сервис доступен для подписок и pay-as-you-go
- `deprecated` — устарел, новые подписки нельзя оформить, существующие работают до конца периода
- `archived` — удалён, не отображается в API, исторические данные сохранены

### 3. `tariffs` — Тарифы подписок

```sql
-- Справочник тарифов (неизменяемый код)
CREATE TABLE tariffs (
    id SMALLSERIAL PRIMARY KEY,
    code VARCHAR(50) UNIQUE NOT NULL,  -- free, basic, pro (для кода)
    name VARCHAR(255) NOT NULL,
    description TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Версии тарифов (цены и условия)
CREATE TABLE tariff_versions (
    id SERIAL PRIMARY KEY,
    tariff_id SMALLINT REFERENCES tariffs(id),
    
    valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until TIMESTAMPTZ,         -- NULL = действует бесконечно
    
    duration_days INTEGER NOT NULL DEFAULT 30,
    base_price_rub NUMERIC(10, 2) NOT NULL,
    prepaid_amount_rub NUMERIC(10, 2) NOT NULL,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    -- updated_at нет - версии immutable после создания
    
    UNIQUE (tariff_id, valid_from)
);

-- Индекс для поиска актуальной версии
CREATE INDEX idx_tariff_versions_validity ON tariff_versions(tariff_id, valid_from, valid_until);

-- Проверка: непересечение версий
CREATE OR REPLACE FUNCTION check_tariff_version_overlap()
RETURNS TRIGGER AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM tariff_versions
        WHERE tariff_id = NEW.tariff_id
          AND valid_from < COALESCE(NEW.valid_until, 'infinity'::timestamptz)
          AND COALESCE(valid_until, 'infinity'::timestamptz) > NEW.valid_from
          AND id != NEW.id
    ) THEN
        RAISE EXCEPTION 'Tariff version overlaps with existing record';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER prevent_tariff_version_overlap
    BEFORE INSERT OR UPDATE ON tariff_versions
    FOR EACH ROW EXECUTE FUNCTION check_tariff_version_overlap();
```

### 4. `tariff_service_prices` — Цены операций

```sql
CREATE TABLE tariff_service_prices (
    id SERIAL PRIMARY KEY,
    tariff_version_id INTEGER REFERENCES tariff_versions(id),
    service_id VARCHAR(50) REFERENCES services(id),
    
    included_price_rub NUMERIC(10, 2) NOT NULL,
    overage_price_rub NUMERIC(10, 2) NOT NULL,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    -- Одна версия тарифа + сервис = одна цена
    UNIQUE (tariff_version_id, service_id)
);

-- Индекс для поиска цены по версии тарифа
CREATE INDEX idx_tariff_prices_version ON tariff_service_prices(tariff_version_id, service_id);
```

### 5. `subscriptions` — Подписки

```sql
CREATE TYPE subscription_status AS ENUM (
    'active', 'grace_period', 'expired', 
    'pending_downgrade', 'upgraded', 'cancelled'
);

CREATE TABLE subscriptions (
    id SERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),      -- Ссылка на accounts.id
    tariff_version_id INTEGER REFERENCES tariff_versions(id),
    status subscription_status DEFAULT 'active',
    
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    grace_period_ends_at TIMESTAMPTZ,
    
    initial_prepaid_rub NUMERIC(10, 2) NOT NULL,  
    -- ВАЖНО: Это СНАПШОТ на момент создания! 
    -- Текущий остаток = SUM(prepaid_amount_rub) из billing_events по подписке
    -- Не использовать для проверки баланса — считать из событий!
    
    auto_renew BOOLEAN DEFAULT FALSE,
    next_tariff_version_id INTEGER REFERENCES tariff_versions(id),
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_subscriptions_account_active 
ON subscriptions(account_id, status) 
WHERE status IN ('active', 'grace_period');
```

### 6. `billing_events` — Event Sourcing

```sql
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

CREATE TABLE billing_events (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),      -- Ключ на accounts.id
    subscription_id INTEGER REFERENCES subscriptions(id),
    service_id VARCHAR(50) REFERENCES services(id),
    
    type billing_event_type NOT NULL,
    
    real_amount_rub NUMERIC(10, 2) DEFAULT 0.00,
    prepaid_amount_rub NUMERIC(10, 2) DEFAULT 0.00,
    
    -- Идемпотентность (NULL для внутренних событий: cron, expired)
    request_id VARCHAR(100) UNIQUE,
    
    metadata JSONB DEFAULT '{}',
    
    created_at TIMESTAMPTZ DEFAULT NOW()
    -- updated_at нет - события immutable!
);

-- Генерация UUID для событий без request_id
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

CREATE INDEX idx_billing_events_account_time 
ON billing_events(account_id, created_at);
```

### 7. `balance_snapshots` — Снимки балансов

```sql
CREATE TABLE balance_snapshots (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id),
    real_balance_rub NUMERIC(10, 2) DEFAULT 0.00,
    prepaid_balance_rub NUMERIC(10, 2) DEFAULT 0.00,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Таблица активных резервов (PENDING-транзакции)
CREATE TABLE reservations (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    subscription_id INTEGER REFERENCES subscriptions(id),
    service_id VARCHAR(50) REFERENCES services(id),
    
    request_id VARCHAR(100) UNIQUE NOT NULL,
    amount_rub NUMERIC(10, 2) NOT NULL,
    charge_type VARCHAR(20) NOT NULL,  -- 'prepaid' | 'pay_as_you_go'
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL    -- auto-cleanup через 5 минут
);

CREATE INDEX idx_reservations_account ON reservations(account_id);
CREATE INDEX idx_reservations_expires ON reservations(expires_at);
```

### 8. `subscription_changes` — Audit

```sql
CREATE TABLE subscription_changes (
    id SERIAL PRIMARY KEY,
    subscription_id INTEGER REFERENCES subscriptions(id),
    change_type VARCHAR(20) CHECK (change_type IN ('upgrade', 'downgrade', 'renewal')),
    from_tariff_version_id INTEGER REFERENCES tariff_versions(id),
    to_tariff_version_id INTEGER REFERENCES tariff_versions(id),
    prorated_amount_rub NUMERIC(10, 2),
    payment_method VARCHAR(20),
    created_at TIMESTAMPTZ DEFAULT NOW()
    -- updated_at нет - история immutable!
);
```

---

## API (только REST для MVP)

**Решение:** Используем только HTTP REST. gRPC добавим позже при необходимости.

**Источник истины:** Таблица `billing_events` — immutable лог всех операций. `balance_snapshots` — только кэш для чтения. `reservations` — активные PENDING-резервы.

### Аутентификация между сервисами

**Механизм:** Статические service tokens (хранятся в Yandex Lockbox).

**Заголовок:** `Authorization: Bearer <service_token>`

**Токены и права:**
| Токен | Доступные эндпоинты |
|-------|---------------------|
| `CABINET_TOKEN` | POST /accounts, GET/POST /accounts/{id}/* |
| `ORCHESTRATOR_TOKEN` | POST /accounts/{id}/reserve, POST /transactions/{id}/commit/rollback |
| `ADMIN_TOKEN` | Все эндпоинты + POST /transactions/{id}/refund |
| `CRON_TOKEN` | Внутренние эндпоинты для cron jobs |

### Внутренние эндпоинты (для Cabinet, Orchestrator, API Gateway)

**Billing Service не доступен напрямую извне.** Работает только внутри Docker-сети по адресу `http://billing:8080`. Внешние запросы маршрутизируются через API Gateway (`api.adocs.ru`) или Cabinet (`lk.adocs.ru`), которые проксируют вызовы во внутреннюю сеть.

#### Счета

```http
POST /accounts
Content-Type: application/json
Authorization: Bearer <CABINET_TOKEN>

{}  -- пустое тело, Billing не знает о клиентах

Response 201:
{
  "id": 456,                    -- billing_account_id
  "created_at": "2025-03-15T10:00:00Z"
}

-- Cabinet сохраняет маппинг org.id -> billing_account_id = 456 у себя
```

#### Баланс

```http
GET /accounts/456/balance  -- billing_account_id
Authorization: Bearer <service_token>

Response 200:
{
  "account_id": 456,
  "real_balance_rub": 1500.00,
  "prepaid_balance_rub": 2450.50,
  "active_subscription": {
    "id": 456,
    "tariff_id": "pro",
    "tariff_name": "Про",
    "expires_at": "2025-04-15T10:00:00Z",
    "status": "active"
  },
  "calculated_at": "2025-03-15T10:05:00Z"
}
```

#### Пополнение (через ЮКасса)

```http
POST /accounts/456/topups  -- billing_account_id
Content-Type: application/json

{
  "amount_rub": 5000.00,
  "payment_method": "card",  -- или "invoice"
  "return_url": "https://lk.adocs.ru/payment/success"
}

Response 201:
{
  "payment_id": "pay_abc123",
  "status": "pending",
  "confirmation_url": "https://yookassa.ru/..."
}
```

#### Webhook от ЮКассы

```http
POST /webhooks/yookassa
Content-Type: application/json

{
  "event": "payment.succeeded",
  "object": {
    "id": "pay_abc123",
    "amount": {"value": "5000.00"},
    "metadata": {"account_id": "123"}
  }
}

Response 200: {"status": "processed"}
```

#### Получение активной подписки

```http
GET /accounts/456/subscriptions  -- billing_account_id
Authorization: Bearer <service_token>

Response 200:
{
  "subscription_id": 456,
  "account_id": 456,
  "tariff_code": "pro",
  "tariff_name": "Pro",
  "status": "active",
  "started_at": "2025-03-15T10:00:00Z",
  "expires_at": "2025-04-15T10:00:00Z",
  "auto_renew": true,
  "initial_prepaid_rub": 5000.00
}

Response 404: {"error":"no active subscription"}
```

**Назначение:** Cabinet Service вызывает для отображения текущего тарифа в личном кабинете. Если подписки нет — клиент видит базовый тариф `basic` (pay-as-you-go).

#### Покупка подписки

```http
POST /accounts/456/subscriptions  -- billing_account_id
Content-Type: application/json
Authorization: Bearer <service_token>

{
  "tariff_code": "pro",
  "payment_method": "balance"  -- или "card"
}

Response 201:
{
  "subscription_id": 456,
  "status": "active",
  "amount_charged_rub": 10000.00
}
```

#### Upgrade

```http
POST /accounts/456/subscriptions/upgrade  -- billing_account_id
Content-Type: application/json

{
  "tariff_id": "enterprise",
  "payment_method": "balance"
}

Response 200:
{
  "subscription_id": 456,
  "previous_tariff": "pro",
  "new_tariff": "enterprise",
  "prorated_bonus_rub": 2666.67,
  "expires_at": "2025-04-15T10:00:00Z"  -- та же дата!
}
```

**Алгоритм расчёта бонуса при апгрейде:**

```
Исходные данные:
- Текущий тариф: Pro (6000₽ prepaid, 20000₽/мес)
- Новый тариф: Enterprise (15000₽ prepaid, 50000₽/мес)
- Осталось дней: 20 из 30

Шаг 1: Считаем стоимость оставшихся дней по старому тарифу
  unused_old = 20000 * (20/30) = 13333.33₽

Шаг 2: Считаем стоимость оставшихся дней по новому тарифу  
  unused_new = 50000 * (20/30) = 33333.33₽

Шаг 3: Разница = доплата за апгрейд
  upgrade_cost = unused_new - unused_old = 20000.00₽

Шаг 4: Бонус = разница в prepaid с пропорцией
  prepaid_diff = 15000 - 6000 = 9000₽
  prorated_bonus = 9000 * (20/30) = 6000₽

Шаг 5: Итоговый платёж
  total_charge = upgrade_cost - prorated_bonus = 20000 - 6000 = 14000₽

Итог:
- С клиента списывается 14000₽ с баланса
- На prepaid начисляется 15000₽ (полный новый лимит)
- Дата окончания подписки НЕ меняется (тот же 15 апреля)
- При следующем продлении — уже полная цена Enterprise (50000₽)
```

**Упрощённая формула:**
```
total_charge = (new_monthly - old_monthly) * (days_remaining / 30)
             - (new_prepaid - old_prepaid) * (days_remaining / 30)

Если total_charge < 0 → бонус на баланс (редкий случай downgrade по цене)
Если total_charge > 0 → списание с баланса
```

---

## История цен и тарифов

### Изменение цены операции

```sql
-- Получаем ID тарифа Pro и текущей версии
SELECT t.id, tv.id INTO @tariff_id, @version_id
FROM tariffs t
JOIN tariff_versions tv ON tv.tariff_id = t.id
WHERE t.code = 'pro' AND tv.valid_until IS NULL;

-- Сегодня: цена passport_rf = 1.00 / 5.00
INSERT INTO tariff_service_prices 
(tariff_version_id, service_id, included_price_rub, overage_price_rub)
VALUES (@version_id, 'passport_rf', 1.00, 5.00);

-- Через 3 месяца подорожали
-- 1. Закрываем текущую версию
UPDATE tariff_versions SET valid_until = '2025-03-31 23:59:59' WHERE id = @version_id;

-- 2. Создаем новую версию
INSERT INTO tariff_versions (tariff_id, valid_from, duration_days, base_price_rub, prepaid_amount_rub)
VALUES (@tariff_id, '2025-04-01', 30, 25000.00, 8000.00)
RETURNING id INTO @new_version_id;

-- 3. Новые цены
INSERT INTO tariff_service_prices (tariff_version_id, service_id, included_price_rub, overage_price_rub)
VALUES (@new_version_id, 'passport_rf', 1.50, 7.00);
```

### Поиск актуальной цены

```sql
-- Цена на конкретную дату
SELECT tsp.included_price_rub, tsp.overage_price_rub
FROM tariff_service_prices tsp
JOIN tariff_versions tv ON tv.id = tsp.tariff_version_id
JOIN tariffs t ON t.id = tv.tariff_id
WHERE t.code = 'pro'
  AND tsp.service_id = 'passport_rf'
  AND tv.valid_from <= '2025-02-15'
  AND (tv.valid_until IS NULL OR tv.valid_until > '2025-02-15');
```

### Новая версия тарифа

```sql
-- 1. Создаём справочник тарифа Pro (один раз)
INSERT INTO tariffs (code, name, description)
VALUES ('pro', 'Про', 'Профессиональный тариф с предоплаченными операциями');

-- 2. Создаём версию Pro v1 (до 31 марта 2025)
INSERT INTO tariff_versions 
(tariff_id, valid_from, valid_until, duration_days, base_price_rub, prepaid_amount_rub)
VALUES (
    (SELECT id FROM tariffs WHERE code = 'pro'),
    '2024-01-01', '2025-03-31', 30, 20000.00, 6000.00
);

-- 3. Создаём версию Pro v2 (с 1 апреля 2025 - дороже и больше кредитов)
INSERT INTO tariff_versions 
(tariff_id, valid_from, valid_until, duration_days, base_price_rub, prepaid_amount_rub)
VALUES (
    (SELECT id FROM tariffs WHERE code = 'pro'),
    '2025-04-01', NULL, 30, 25000.00, 8000.00
);

-- Старые подписки (до 31 марта) работают по старым версиям
-- Новые подписки (с 1 апреля) - по новым
```

---

## Жизненный цикл

### 1. Создание счета (Cabinet → Billing)

```http
POST /accounts
Content-Type: application/json
Authorization: Bearer <CABINET_TOKEN>

{}  -- пустое тело, Billing не знает о клиентах

Response 201:
{
  "id": 456,                    -- billing_account_id
  "created_at": "2025-03-15T10:00:00Z"
}

-- Cabinet сохраняет маппинг org.id -> billing_account_id = 456 у себя
-- Стартовый баланс 0, подписка NULL
```

### 2. Покупка подписки

**Вариант А: С баланса (если есть деньги)**
```http
POST /accounts/456/subscriptions
Authorization: Bearer <CABINET_TOKEN>
Content-Type: application/json

{
  "tariff_code": "pro",
  "payment_method": "balance"
}

-- Проверяем real_balance >= base_price_rub
-- billing_events:
--   1. type='subscription_charge', real_amount_rub=-base_price_rub
--   2. type='upgrade_bonus', prepaid_amount_rub=+prepaid_amount_rub (если тариф включает prepaid)
-- subscriptions: создаем запись
```

**Вариант Б: С карты (через ЮКассу)**
```http
POST /accounts/456/topups  -- сначала пополняем
POST /accounts/456/subscriptions  -- потом покупаем
```

### 3. Операция распознавания (Orchestrator → Billing)

```http
// 1. Reserve (блокировка баланса)
POST /accounts/456/reserve
Authorization: Bearer <ORCHESTRATOR_TOKEN>
Content-Type: application/json
X-Idempotency-Key: req_uuid

{
  "service_id": "passport_rf",
  "request_id": "req_uuid"
}

Response 200:
{
  "reserved": true,
  "transaction_id": "txn_789",
  "charge_type": "prepaid",
  "amount_rub": 4.00
}

// 2. После успешного OCR - Commit
POST /transactions/txn_789/commit
Authorization: Bearer <ORCHESTRATOR_TOKEN>

// 2. При ошибке OCR - Rollback  
POST /transactions/txn_789/rollback
Authorization: Bearer <ORCHESTRATOR_TOKEN>
{
  "reason": "ocr_failed"
}
```

**Алгоритм Reserve (с защитой от race condition):**
```sql
BEGIN;

-- 1. Проверяем статус аккаунта
SELECT status FROM accounts 
WHERE id = :account_id 
FOR UPDATE;

-- Если status != 'active' → ошибка (402 если blocked, 404 если archived)

-- 1.5. Проверяем подписку для определения charge_type
-- active подписка → prepaid (списываем с предоплаты)
-- grace_period или нет подписки → pay_as_you_go (нужен положительный баланс)
SELECT status INTO :sub_status
FROM subscriptions 
WHERE account_id = :account_id 
  AND status IN ('active', 'grace_period')
ORDER BY 
    CASE status WHEN 'active' THEN 1 WHEN 'grace_period' THEN 2 END,
    expires_at DESC
LIMIT 1;

-- Если sub_status = 'grace_period' и charge_type = 'pay_as_you_go' 
-- → проверяем что real_balance > 0 ( prepaid не работает в grace_period )

-- 2. Блокируем строку баланса
SELECT * FROM balance_snapshots 
WHERE account_id = :account_id 
FOR UPDATE;

-- 3. Считаем доступный баланс:
--    snapshot (подтвержденный) 
--    + события после snapshot (неподтвержденные) 
--    - активные резервы
SELECT 
    bs.real_balance_rub 
    + COALESCE(SUM(be.real_amount_rub) FILTER (WHERE be.created_at > bs.updated_at), 0)
    - COALESCE(SUM(r.amount_rub) FILTER (WHERE r.charge_type = 'pay_as_you_go'), 0) as available_real,
    
    bs.prepaid_balance_rub 
    + COALESCE(SUM(be.prepaid_amount_rub) FILTER (WHERE be.created_at > bs.updated_at), 0)
    - COALESCE(SUM(r.amount_rub) FILTER (WHERE r.charge_type = 'prepaid'), 0) as available_prepaid
FROM balance_snapshots bs
LEFT JOIN billing_events be ON be.account_id = bs.account_id AND be.created_at > bs.updated_at
LEFT JOIN reservations r ON r.account_id = bs.account_id AND r.expires_at > NOW()
WHERE bs.account_id = :account_id
GROUP BY bs.account_id, bs.real_balance_rub, bs.prepaid_balance_rub, bs.updated_at;

-- 4. Определяем charge_type и проверяем достаточность
-- IF sub_status = 'active' AND available_prepaid >= price → charge_type = 'prepaid'
-- ELSE → charge_type = 'pay_as_you_go', проверяем available_real >= price
--      В grace_period: prepaid не работает, но можно списать с рублёвого баланса
--      Без подписки: только pay_as_you_go с рублёвого баланса

-- 5. Создаем резерв
INSERT INTO reservations (account_id, service_id, request_id, amount_rub, charge_type, expires_at)
VALUES (:account_id, 'passport_rf', 'req_uuid', 4.00, :charge_type, NOW() + INTERVAL '5 minutes');

COMMIT;
```

**Почему так:**
- `balance_snapshots` — кэш баланса; при пересчёте (`RecalculateBalance`) события суммируются и записываются в снапшот
- При записи снапшота `updated_at` устанавливается на `NOW() - 1 микросекунду`. Это гарантирует, что любое событие, созданное в той же транзакции/микросекунде, будет строго позже снапшота и попадёт в пересчёт при следующем `GetBalance` через `created_at > updated_at`
- `billing_events` с момента последнего снапшота = десятки записей, не тысячи — производительность ОК
- `reservations` — активные PENDING-резервы, которые ещё не стали событиями

### 4. Жизненный цикл подписки (cron jobs)

**Запуск:** Каждые 10 минут

**Важно:** Шаги 4.1, 4.2, 4.3 должны выполняться последовательно в одном cron-запуске (в одной транзакции или последовательно), иначе возможны гонки между статусами.

#### 4.1 Переход `active` → `grace_period`
```sql
UPDATE subscriptions 
SET status = 'grace_period',
    grace_period_ends_at = expires_at + INTERVAL '7 days'
WHERE status = 'active' 
  AND expires_at <= NOW()
  AND grace_period_ends_at IS NULL;
```

#### 4.2 Переход `grace_period` → `expired`
```sql
UPDATE subscriptions 
SET status = 'expired'
WHERE status = 'grace_period' 
  AND grace_period_ends_at <= NOW();

-- Списание остатка prepaid (если есть) — списывается в пользу сервиса
-- Остаток = сумма всех prepaid-событий по подписке
INSERT INTO billing_events (account_id, subscription_id, type, prepaid_amount_rub)
SELECT 
    s.account_id, 
    s.id, 
    'subscription_expired', 
    -GREATEST(0, COALESCE(SUM(be.prepaid_amount_rub), 0))  -- сжигаем остаток
FROM subscriptions s
LEFT JOIN billing_events be ON be.subscription_id = s.id 
    AND be.type IN ('subscription_charge', 'upgrade_bonus', 'prepaid_usage')
WHERE s.status = 'expired' 
  AND NOT EXISTS (
      -- Проверяем что ещё не списывали остаток
      SELECT 1 FROM billing_events be2 
      WHERE be2.subscription_id = s.id 
        AND be2.type = 'subscription_expired'
  )
GROUP BY s.id, s.account_id
HAVING COALESCE(SUM(be.prepaid_amount_rub), 0) > 0;
```

**Grace Period (7 дней):**
- Подписка истекла, но предоплаченные операции ещё работают (prepaid сгорает после grace period)
- Pay-as-you-go по подписке заблокирован — нельзя списывать с prepaid
- Pay-as-you-go с рублёвого баланса разрешён — если `available_real > 0`, операция пройдёт
- Клиент получает уведомление о необходимости продления

#### 4.3 Обработка `pending_downgrade`
```sql
-- При окончании текущей подписки создаём новую с downgrade тарифом
-- (не UPDATE, а INSERT — чтобы сохранить историю и правильный initial_prepaid_rub)
INSERT INTO subscriptions (
    account_id, tariff_version_id, status, 
    started_at, expires_at, grace_period_ends_at,
    initial_prepaid_rub, auto_renew
)
SELECT 
    s.account_id, 
    s.next_tariff_version_id, 
    'active',
    NOW(),
    NOW() + tv.duration_days * INTERVAL '1 day',
    NULL,
    tv.prepaid_amount_rub,  -- правильный initial_prepaid_rub нового тарифа
    FALSE
FROM subscriptions s
JOIN tariff_versions tv ON tv.id = s.next_tariff_version_id
WHERE s.status = 'expired' 
  AND s.next_tariff_version_id IS NOT NULL;

-- Помечаем старую подписку как обработанную
UPDATE subscriptions 
SET next_tariff_version_id = NULL
WHERE status = 'expired' 
  AND next_tariff_version_id IS NOT NULL;
```

---

## Интеграция с Cabinet Service

### Создание организации

```go
// Cabinet Service
type Organization struct {
    ID               int64
    Name             string
    Email            string
    BillingAccountID int64  -- ссылка на Billing
}

func CreateOrg(name, email string) {
    // 1. Создаем в своей БД
    org := db.CreateOrganization(name, email)
    
    // 2. Создаем счет в Billing (HTTP POST)
    resp, _ := http.Post("http://billing:8080/v1/accounts", 
        "application/json", 
        bytes.NewBuffer([]byte(`{}`))  -- пустое тело
    )
    var result struct{ Id int64 }
    json.NewDecoder(resp.Body).Decode(&result)
    
    // 3. Сохраняем billing_account_id в Cabinet
    org.UpdateBillingAccountID(result.Id)  -- billing_account_id = 456
}

// При вызове операций используем billing_account_id:
func GetBalance(orgID int64) {
    org := db.GetOrganization(orgID)
    
    // HTTP GET в Billing
    resp, _ := http.Get(fmt.Sprintf(
        "http://billing:8080/v1/accounts/%d/balance", 
        org.BillingAccountID,
    ))
    // ...
}
```

### Получение баланса и подписки в ЛК

```go
// Cabinet Service вызывает Billing (HTTP)
resp, _ := http.Get(fmt.Sprintf("http://billing:8080/v1/accounts/%d/balance", accountID))
var balance struct {
    RealBalanceRub    float64 `json:"real_balance_rub"`
    PrepaidBalanceRub float64 `json:"prepaid_balance_rub"`
}
json.NewDecoder(resp.Body).Decode(&balance)
// Отображает в UI

// Получение активной подписки
resp, _ := http.Get(fmt.Sprintf("http://billing:8080/v1/accounts/%d/subscriptions", accountID))
var sub struct {
    TariffCode string `json:"tariff_code"`
    Status     string `json:"status"`
    ExpiresAt  string `json:"expires_at"`
    AutoRenew  bool   `json:"auto_renew"`
}
json.NewDecoder(resp.Body).Decode(&sub)
// Отображает текущий тариф и кнопку "Сменить тариф"
```

---

## Docker Compose (отдельные БД)

```yaml
version: '3.8'

services:
  # === Main API Database ===
  postgres-main:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: api_scan_main
      POSTGRES_USER: api_scan
    volumes:
      - main_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  # === Billing Database (отдельная!) ===
  postgres-billing:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: billing_db
      POSTGRES_USER: billing
    volumes:
      - billing_data:/var/lib/postgresql/data
      - ./services/billing/migrations:/docker-entrypoint-initdb.d
    ports:
      - "5433:5432"

  # === Services ===
  orchestrator:
    build: ./services/orchestrator
    environment:
      DATABASE_URL: postgres://api_scan@postgres-main:5432/api_scan_main
      BILLING_API_URL: http://billing:8080
      
  billing:
    build: ./services/billing
    environment:
      DATABASE_URL: postgres://billing@postgres-billing:5432/billing_db
      PORT: 8080
    ports:
      - "8081:8080"    -- HTTP REST
      
  cabinet:
    build: ./services/cabinet
    environment:
      DATABASE_URL: postgres://api_scan@postgres-main:5432/api_scan_main
      BILLING_API_URL: http://billing:8080

volumes:
  main_data:
  billing_data:
```

---

## Аудит и наблюдаемость

### Триггеры для updated_at

```sql
-- Автоматическое обновление updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Применяем к таблицам
CREATE TRIGGER update_accounts_updated_at BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_tariffs_updated_at BEFORE UPDATE ON tariffs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_subscriptions_updated_at BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
```

### Просмотр истории изменений

```sql
-- Когда менялся тариф подписки?
SELECT 
    sc.change_type,
    sc.from_tariff_version_id,
    sc.to_tariff_version_id,
    t1.code as from_tariff_code,
    t2.code as to_tariff_code,
    sc.prorated_amount_rub,
    sc.created_at
FROM subscription_changes sc
JOIN tariff_versions tv1 ON tv1.id = sc.from_tariff_version_id
JOIN tariff_versions tv2 ON tv2.id = sc.to_tariff_version_id
JOIN tariffs t1 ON t1.id = tv1.tariff_id
JOIN tariffs t2 ON t2.id = tv2.tariff_id
WHERE sc.subscription_id = 123
ORDER BY sc.created_at DESC;

-- Как менялся баланс счета?
SELECT 
    type,
    real_amount_rub,
    prepaid_amount_rub,
    created_at,
    metadata
FROM billing_events
WHERE account_id = 456
ORDER BY created_at DESC;

-- Все цены на passport_rf за последний год
SELECT 
    tv.valid_from,
    tv.valid_until,
    tsp.included_price_rub,
    tsp.overage_price_rub,
    tsp.created_at
FROM tariff_service_prices tsp
JOIN tariff_versions tv ON tv.id = tsp.tariff_version_id
JOIN tariffs t ON t.id = tv.tariff_id
WHERE t.code = 'pro' AND tsp.service_id = 'passport_rf'
ORDER BY tv.valid_from DESC;

-- Поиск по metadata: платежи через ЮКасса
SELECT *
FROM billing_events
WHERE type = 'balance_topup'
  AND metadata->>'source' = 'yookassa'
  AND created_at > NOW() - INTERVAL '7 days';

-- Статистика по сервисам из metadata
SELECT 
    metadata->>'service' as service,
    COUNT(*) as operations,
    SUM(prepaid_amount_rub) as total_prepaid
FROM billing_events
WHERE type = 'prepaid_usage'
  AND created_at > NOW() - INTERVAL '30 days'
GROUP BY metadata->>'service';
```

### Мониторинг изменений

```sql
-- Подписки, измененные за последний час
SELECT * FROM subscriptions 
WHERE updated_at > NOW() - INTERVAL '1 hour';

-- Версии тарифов с истекающим сроком действия
SELECT t.code, tv.valid_until 
FROM tariff_versions tv
JOIN tariffs t ON t.id = tv.tariff_id
WHERE tv.valid_until IS NOT NULL 
  AND tv.valid_until < NOW() + INTERVAL '7 days';
```

---

## Преимущества отдельного Billing

1. **Переиспользование** — можно подключить к image_generator, другим проектам
2. **Масштабирование** — вынести на отдельный сервер без изменений
3. **SaaS потенциал** — "Billing as a Service"
4. **Изоляция** — изменения в биллинге не ломают основной сервис
5. **Команда** — отдельная команда может развивать биллинг

---

## Что дальше?

1. Создать структуру `services/billing/`
2. Написать миграции для `billing_db`
3. Реализовать HTTP handlers (только REST для MVP)
4. Интегрировать с Cabinet (создание account)
5. Интегрировать с Orchestrator (Reserve/Commit)

Готов начать реализацию?