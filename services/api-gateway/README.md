# API Gateway

Единая точка входа для всех API запросов. Аутентификация, rate limiting, CORS, маршрутизация.

## Архитектура

```
Client → CORS → Logging → Auth → RateLimit → Routing → Service
```

## Функциональность

### Аутентификация (API Keys)

- Проверка заголовка `X-Api-Key`
- Формат: `base64(key_id:secret)`
- Хеширование: bcrypt
- Redis cache для производительности

### Rate Limiting

- Sliding window algorithm
- 10 RPS default per API key
- Redis-based

### CORS

- Настроен для браузерных клиентов
- Разрешены origins: `*` (для dev)
- Поддерживаются credentials

### Логирование

Все запросы логируются:
```
[timestamp] METHOD /path - status (bytes) - duration - remote_addr
```

Пример:
```
[2026-03-18 16:00:00] GET /v1/billing/accounts/1/balance - 200 (89 bytes) - 12ms - 172.20.0.1
```

## API Endpoints

| Метод | Путь | Auth | Описание |
|-------|------|------|----------|
| GET | `/health` | — | Health check |
| GET | `/swagger/` | — | Swagger UI |
| ALL | `/v1/recognize` | API Key | Распознавание паспорта |
| ALL | `/v1/billing/*` | API Key | Billing operations |
| ALL | `/v1/...` | API Key | Прочие сервисы |
| POST | `/webhooks/yookassa` | — | YooKassa webhooks |

## Маршрутизация

| Путь | Destination |
|------|-------------|
| `/v1/recognize` | Orchestrator |
| `/v1/billing/*` | Billing Service |
| `/webhooks/yookassa` | Billing Webhook |

## Конфигурация

| Переменная | Описание | Значение по умолчанию |
|------------|----------|----------------------|
| `PORT` | Порт сервера | `8080` |
| `DATABASE_URL` | PostgreSQL (API keys) | — |
| `REDIS_ADDR` | Redis host:port | `localhost:6379` |
| `ORCHESTRATOR_URL` | URL Orchestrator | `http://localhost:8083` |
| `BILLING_URL` | URL Billing | `http://localhost:8081` |
| `BILLING_WEBHOOK_URL` | URL Billing Webhook | `http://localhost:8082` |

## Разработка

```bash
# Запуск
go run ./cmd/server/main.go

# Тестирование
curl http://localhost:8080/health
```

## Swagger

Документация API: http://localhost:8080/swagger/
