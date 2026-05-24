# Local Testing Guide

Полное руководство по локальному запуску и тестированию системы.

## 🌐 Личный Кабинет (Web UI)

Откройте в браузере: **http://localhost:8084/**

Функции ЛК:
- 🔐 Вход/регистрация
- 📊 Просмотр баланса и статистики
- 🔑 Управление API ключами (создание, отзыв)
- 💰 История операций

Тестовые данные:
- **Email**: `test@example.com`
- **Password**: `password`

---

## Быстрый старт (5 минут)

```bash
# 1. Запуск инфраструктуры
docker-compose up -d postgres postgres-billing redis

# 2. Применение миграций
cd migrations/main && goose up
cd ../billing && goose up

# 3. Заполнение тестовыми данными (тарифы, тестовый пользователь)
docker exec -i api-scan-postgres psql -U api_scan -d api_scan < scripts/seed.sql
docker exec -i api-scan-postgres-billing psql -U billing -d billing_db < scripts/seed.sql

# 4. Запуск всех сервисов
docker-compose --profile billing --profile gateway --profile cabinet up -d

# 5. Проверка статуса
docker-compose ps
```

## Тестовые данные

После seed-скрипта доступны:

| Ресурс | Значение | Описание |
|--------|----------|----------|
| **Email** | `test@example.com` | Логин |
| **Password** | `password` | Пароль |
| **Org ID** | `1` | ID организации |
| **Billing Acc** | `1` | ID счёта биллинга |
| **Тариф Free** | `1000₽ prepaid` | Бесплатный с предоплатой |
| **Тариф Pro** | `20000₽/мес + 6000₽ prepaid` | Подписка |

## Пошаговое тестирование

### 1. Вход в кабинет

```bash
# Login
curl -X POST http://localhost:8084/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "password"
  }'

# Response:
# {
#   "session_token": "abc123...",
#   "user": {"id": 1, "email": "test@example.com", "role": "admin"}
# }
```

### 2. Создание API ключа

```bash
# Сохраните session_token из предыдущего шага
SESSION_TOKEN="your_session_token"

# Create API key
curl -X POST http://localhost:8084/api/v1/api-keys \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $SESSION_TOKEN" \
  -d '{"name": "Test Key"}'

# Response:
# {
#   "id": 1,
#   "name": "Test Key",
#   "full_key": "test_xxx...",  <-- ВАЖНО: сохраните!
#   "rate_limit_rps": 10
# }
```

**Сохраните `full_key` - он показывается только один раз!**

### 3. Проверка баланса

```bash
# Запрос баланса через API Gateway
API_KEY="your_api_key"

curl -X GET "http://localhost:8080/v1/billing/accounts/1/balance" \
  -H "X-Api-Key: $API_KEY"

# Response:
# {
#   "account_id": 1,
#   "real_balance_rub": 0,
#   "prepaid_balance_rub": 1000  <-- Предоплаченные средства
# }
```

### 4. Операция распознавания (с биллингом)

Создадим тестовое изображение:

```bash
# Создаем тестовый файл (или используйте реальный passport.jpg)
echo "test_image_data" > /tmp/test_passport.txt

# Запрос на распознавание
curl -X POST http://localhost:8080/v1/recognize \
  -H "X-Api-Key: $API_KEY" \
  -H "Idempotency-Key: test-req-001" \
  -F "file=@/tmp/test_passport.txt"

# Response (mock):
# {
#   "request_id": "test-req-001",
#   "document_type": "passport_rf",
#   "fields": {...},
#   "confidences": {...},
#   "provider": "yandex"
# }
```

**Проверка резервирования средств:**

```bash
# Проверьте баланс снова - prepaid должен уменьшиться
curl -X GET "http://localhost:8080/v1/billing/accounts/1/balance" \
  -H "X-Api-Key: $API_KEY"
```

### 5. Пополнение баланса (мок ЮКассы)

Так как ЮКасса требует публичный URL, используем прямое пополнение:

```bash
# Через SQL в billing_db
docker exec -it api-scan-postgres-billing psql -U billing -d billing_db -c "
  INSERT INTO billing_events (account_id, type, real_amount_rub, prepaid_amount_rub, created_at)
  VALUES (1, 'balance_topup', 5000, 0, NOW());
"

# Проверьте баланс
curl -X GET "http://localhost:8080/v1/billing/accounts/1/balance" \
  -H "X-Api-Key: $API_KEY"
```

### 6. Покупка подписки Pro

```bash
# Создание подписки через Billing API напрямую
curl -X POST http://localhost:8081/accounts/1/subscriptions \
  -H "Content-Type: application/json" \
  -d '{
    "tariff_code": "pro",
    "payment_method": "balance"
  }'

# Response:
# {
#   "id": 1,
#   "tariff_code": "pro",
#   "status": "active",
#   "prepaid_remaining_rub": 6000
# }
```

### 7. Апгрейд подписки

```bash
# Апгрейд с Free до Pro
curl -X POST http://localhost:8081/accounts/1/subscriptions/upgrade \
  -H "Content-Type: application/json" \
  -d '{
    "tariff_code": "pro",
    "payment_method": "balance"
  }'
```

### 8. Проверка истории операций

```bash
# Проверим события в Cabinet
curl -X GET "http://localhost:8084/api/v1/account-events" \
  -H "Authorization: Bearer $SESSION_TOKEN"
```

## Альтернатива: CLI Admin

```bash
# Установка зависимостей
go mod download

# Создание организации
go run scripts/admin-cli.go create-org

# Создание API ключа
go run scripts/admin-cli.go create-api-key

# Список ключей
go run scripts/admin-cli.go list-api-keys
```

## Проверка через Swagger UI

Откройте в браузере:
- **API Gateway**: http://localhost:8080/swagger/
- **Billing**: http://localhost:8081/swagger/
- **Cabinet**: http://localhost:8084/swagger/

## Логи и мониторинг

### Просмотр логов сервисов

```bash
# Логи конкретного сервиса (фOLLOW режим)
docker-compose logs -f cabinet
docker-compose logs -f api-gateway
docker-compose logs -f billing

# Последние 100 строк
docker-compose logs --tail=100 cabinet

# Все сервисы
docker-compose logs -f

# С фильтром по ошибкам
docker-compose logs -f cabinet | grep -i error
```

### Что логируется

**Cabinet Service (порт 8084):**
```
[2024-03-18 10:15:32] POST /api/v1/auth/login - 200 (245 bytes) - 45ms - 172.20.0.1
[Login] Attempt for email: test@example.com
[Login] Found organization ID: 1, status: active, email_verified: true
[Login] Password verified for user ID: 1
```

**API Gateway (порт 8080):**
```
[2024-03-18 10:16:01] GET /v1/billing/accounts/1/balance - 200 (89 bytes) - 12ms - 172.20.0.1
[Auth] Authenticated org=1 key=1 path=/v1/billing/accounts/1/balance
[Auth] Missing X-Api-Key header from 172.20.0.1
[Auth] Invalid API key format from 172.20.0.1: invalid base64
```

### Формат логов

Каждая строка содержит:
- `[timestamp]` - время запроса
- `METHOD /path` - HTTP метод и путь
- `- status_code` - HTTP статус ответа
- `(bytes)` - размер ответа в байтах
- `- duration` - время обработки
- `- remote_addr` - IP клиента

### Логирование ошибок

При ошибках в логах появляются префиксы:
- `[Auth]` - ошибки аутентификации
- `[Login]` - процесс входа в Cabinet
- `[Billing]` - операции биллинга

---

## Тестирование вебхука ЮКассы (локально)

Так как ЮКасса требует HTTPS и публичный URL, используйте ngrok:

```bash
# 1. Установите ngrok
# https://ngrok.com/download

# 2. Запустите туннель
ngrok http 8082

# 3. Получите URL (например, https://abc123.ngrok.io)
# 4. Настройте вебхук в ЮКассе:
#    URL: https://abc123.ngrok.io/webhooks/yookassa
#    События: payment.succeeded, payment.canceled

# 5. Тестовый запрос
curl -X POST https://abc123.ngrok.io/webhooks/yookassa \
  -H "Content-Type: application/json" \
  -d '{
    "type": "notification",
    "event": "payment.succeeded",
    "object": {
      "id": "test_payment_123",
      "status": "succeeded",
      "amount": {"value": "1000.00", "currency": "RUB"},
      "metadata": {"account_id": "1"}
    }
  }'
```

## Проверка всех сервисов

```bash
# Health checks
curl http://localhost:8080/health  # API Gateway
curl http://localhost:8081/health  # Billing
curl http://localhost:8082/health  # Billing Webhook
curl http://localhost:8083/health  # Orchestrator
curl http://localhost:8084/health  # Cabinet

# Redis
docker exec -it api-scan-redis redis-cli ping

# PostgreSQL
docker exec -it api-scan-postgres pg_isready -U api_scan
docker exec -it api-scan-postgres-billing pg_isready -U billing
```

## Логи и отладка

```bash
# Логи конкретного сервиса
docker-compose logs -f api-gateway
docker-compose logs -f billing
docker-compose logs -f orchestrator

# Все логи
docker-compose logs -f

# Проверка подключения к БД
docker exec -it api-scan-postgres psql -U api_scan -d api_scan -c "\dt"
docker exec -it api-scan-postgres-billing psql -U billing -d billing_db -c "\dt"
```

## Очистка

```bash
# Остановка всех сервисов
docker-compose --profile billing --profile gateway --profile cabinet down

# Остановка с удалением данных
docker-compose --profile billing --profile gateway --profile cabinet down -v

# Перезапуск с чистого листа
docker-compose down -v
docker-compose up -d postgres postgres-billing redis
# ... повторить миграции и seed
```

## Устранение неполадок

### Проблема: "connection refused"
```bash
# Проверьте, что сервисы запущены
docker-compose ps

# Перезапустите проблемный сервис
docker-compose restart billing
```

### Проблема: "authentication failed"
```bash
# Проверьте API ключ
curl -X GET http://localhost:8080/v1/billing/accounts/1/balance \
  -H "X-Api-Key: ваш_ключ" -v

# Проверьте, что ключ активен в БД
docker exec -it api-scan-postgres psql -U api_scan -d api_scan -c \
  "SELECT id, name, status FROM api_keys;"
```

### Проблема: "insufficient balance"
```bash
# Добавьте баланс напрямую в БД
docker exec -it api-scan-postgres-billing psql -U billing -d billing_db -c \
  "INSERT INTO billing_events (account_id, type, real_amount_rub, created_at) 
   VALUES (1, 'balance_topup', 10000, NOW());"
```

## Тестирование демо-деплоя

Демо развёрнуто на сервере `89.223.68.18` (Timeweb).

```bash
# Проверка health через интернет
curl https://api.adocs.ru/health
curl https://lk.adocs.ru/health

# Регистрация нового пользователя (авто-активация аккаунта)
curl -X POST https://lk.adocs.ru/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "password",
    "organization_name": "ООО Пример",
    "accepted_terms": true
  }'

# Вход
curl -X POST https://lk.adocs.ru/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"password"}'

# Создание API ключа (используйте session_token из ответа login)
curl -X POST https://lk.adocs.ru/api/v1/api-keys \
  -H "Authorization: Bearer <session_token>" \
  -H "Content-Type: application/json" \
  -d '{"name": "Demo Key"}'

# Пополнение баланса (demo mock)
curl -X POST https://lk.adocs.ru/api/v1/mock-payments \
  -H "Authorization: Bearer <session_token>" \
  -H "Content-Type: application/json" \
  -d '{"amount_rub": 1000}'

# Проверка баланса через API Gateway
curl https://api.adocs.ru/v1/billing/accounts/me/balance \
  -H "X-Api-Key: <api_key>"
```

**Юридические документы:**
- Политика конфиденциальности: https://lk.adocs.ru/legal/privacy
- Условия использования: https://lk.adocs.ru/legal/terms

---

## SQL-тесты репозиториев

Тесты для PostgreSQL-репозиториев (все 4 сервиса):

```bash
# 1. Поднять PostgreSQL
docker compose -f infra/docker/docker-compose.test.yml up -d postgres postgres-billing

# 2. Применить миграции
cd migrations/main && goose up
cd ../billing && goose up

# 3. Запустить SQL тесты
export TEST_DATABASE_URL=postgres://api_scan:api_scan_secret@localhost:5432/api_scan
export TEST_BILLING_DATABASE_URL=postgres://billing:billing_secret@localhost:5433/billing_db
go test ./services/api-gateway/internal/repository/...
go test ./services/billing/internal/repository/...
go test ./services/billing-webhook-yookassa/internal/repository/...
go test ./services/cabinet/internal/repository/...
```

Если PostgreSQL недоступна, тесты автоматически пропускаются (`t.Skip`).

---

## Следующие шаги

1. ✅ Регистрация и вход работают
2. ✅ API ключи создаются
3. ✅ Биллинг (reserve/commit) работает
4. ✅ Подписки можно создавать
5. ✅ Демо-деплой завершён (SSL, домены, Docker)
6. ✅ SQL-тесты репозиториев написаны
7. ⏳ Продакшен: настроить реальную ЮКассу
8. ⏳ Продакшен: настроить email верификацию
9. ⏳ Продакшен: Yandex Cloud
