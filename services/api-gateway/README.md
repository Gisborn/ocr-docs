# API Gateway

Единая точка входа для всех API запросов. Обеспечивает аутентификацию, rate limiting и маршрутизацию на downstream сервисы.

## Архитектура

```
Client
   │
   ▼
API Gateway (port 8080)
   ├── Auth Middleware (X-Api-Key + bcrypt)
   ├── Rate Limit Middleware (Redis, 10 RPS default)
   └── Proxy
       ├── /v1/recognize ──────▶ Orchestrator
       ├── /v1/billing/* ──────▶ Billing Service
       └── /webhooks/* ────────▶ Billing Webhook YooKassa (no auth)
```

## Формат API Key

API Key имеет формат: `base64(key_id:secret)`

Пример:
- Key ID: `123`
- Secret: `sk_live_abc123xyz`
- API Key: `MTIzOnNrX2xpdmVfYWJjMTIzeHl6` (base64)

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 8080 | Порт сервера |
| `DATABASE_URL` | postgres://... | URL main database (api_scan) |
| `REDIS_ADDR` | localhost:6379 | Адрес Redis |
| `ORCHESTRATOR_URL` | http://localhost:8083 | URL Orchestrator сервиса |
| `BILLING_URL` | http://localhost:8081 | URL Billing сервиса |
| `BILLING_WEBHOOK_URL` | http://localhost:8082 | URL Billing Webhook сервиса |

## Endpoints

### Public (no auth)
- `GET /health` - Health check
- `POST /webhooks/yookassa` - YooKassa webhook proxy

### Protected (auth + rate limit)
- `POST /v1/recognize` - OCR распознавание → Orchestrator
- `GET /v1/billing/accounts/{id}/balance` → Billing
- `POST /v1/billing/accounts/{id}/reserve` → Billing
- `POST /v1/billing/transactions/{id}/commit` → Billing

## Headers

### Request
- `X-Api-Key` - API ключ (base64)
- `X-Request-ID` - ID запроса (auto-generated if missing)

### Response
- `X-RateLimit-Limit` - Лимит запросов (RPS)
- `X-RateLimit-Remaining` - Оставшиеся запросы

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| UNAUTHORIZED | 401 | Отсутствует или невалидный API ключ |
| FORBIDDEN | 403 | Организация заблокирована или ключ отозван |
| RATE_LIMITED | 429 | Превышен лимит запросов |
| NOT_FOUND | 404 | Неизвестный endpoint |
| SERVICE_UNAVAILABLE | 503 | Downstream сервис недоступен |

## Запуск

```bash
# Локально
go run ./services/api-gateway/cmd/server

# Через Docker Compose
docker-compose --profile gateway up -d
```

## Тесты

```bash
go test ./services/api-gateway/... -v
```
