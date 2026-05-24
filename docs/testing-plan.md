# План тестирования API-Scan

> Статус: В работе  
> Приоритет: Биллинг → Cabinet → API Gateway → Orchestrator  
> Цель: ≥ 70% покрытие по критичным сервисам

---

## Текущее состояние

| Компонент | Покрытие | Типы тестов | Приоритет |
|-----------|----------|-------------|-----------|
| `pkg/normalizer` | 49.8% | Unit | Средний |
| `pkg/ocr` | 46.6% | Unit | Средний |
| `services/api-gateway/internal/middleware` | 45.7% | Unit | Средний |
| `services/billing/internal/service` | 60.6% | Unit | **Высокий** |
| `services/billing/internal/handler` | 0% | Unit | **Высокий** |
| `services/billing/internal/repository` | 0% | Unit | **Высокий** |
| `services/billing-webhook-yookassa/internal/handler` | 74.2% | Unit | Низкий |
| `services/orchestrator/internal/handler` | 43.6% | Unit | Средний |
| `services/cabinet` | 0% | — | **Высокий** |
| `services/api-gateway/internal/handler` | 0% | Unit | Средний |

---

## Этап 1: Биллинг (цель: 75%+)

### 1.1 Unit-тесты `billing/internal/handler`

| Хендлер | Что тестируем |
|---------|---------------|
| `Health` | 200 OK |
| `CreateAccount` | 201 + структура ответа |
| `GetBalance` | 200, 400 (invalid ID), 500 |
| `Reserve` | 200 (reserved), 400 (bad JSON), 402 (no balance), идемпотентность |
| `Commit` | 200 (committed), 404 (not found) |
| `Rollback` | 200 (rolled back), 404 |
| `TopupBalance` | 200 (success), 400 (≤0), 500 |
| `CreateSubscription` | 201, 400, 402 (no balance) |
| `Upgrade` | 200, 400, 402 |
| `GetAccountSubscription` | 200, 404 |
| `GetBillingEvents` | 200, 400 |
| `CreatePayment` / `GetPayment` | 201, 400, 404 |

**Подход:** мокаем `BillingService`, `SubscriptionService`, `PaymentService` через интерфейсы.

### 1.2 Дополнительные unit-тесты `billing/internal/service`

| Сценарий | Описание |
|----------|----------|
| `Reserve` — blocked account | ErrAccountBlocked |
| `Reserve` — archived account | ErrAccountArchived |
| `Reserve` — idempotency | Повторный вызов с тем же request_id возвращает тот же результат |
| `Reserve` — prepaid подписка | Списание с prepaid balance, fallback на overage |
| `Reserve` — pay_as_you_go без подписки | Списание с real balance по цене 7 ₽ |
| `Reserve` — expired reservations | Не влияют на доступный баланс |
| `Commit` — повторный commit | 404 (резерв уже удалён) |
| `Rollback` — несуществующий резерв | Ошибка или безопасный no-op |

### 1.3 E2E тест биллинга

Полный сценарий через HTTP:

```
1. Создать billing account (POST /accounts)
2. Пополнить баланс (POST /accounts/{id}/topup)
3. Проверить баланс (GET /accounts/{id}/balance) → ожидаем сумму
4. Зарезервировать средства (POST /accounts/{id}/reserve)
5. Проверить баланс → ожидаем уменьшение (с учётом резерва)
6. Закоммитить (POST /transactions/{id}/commit)
7. Проверить баланс → ожидаем фиксированное списание
8. Попытка повторного commit → 404
9. Rollback нового резерва → баланс восстановлен
10. Получить историю событий (GET /accounts/{id}/events)
```

---

## Этап 2: Cabinet (цель: 60%+)

### 2.1 Unit-тесты

- `internal/handler` — регистрация, логин, сессии, API ключи
- `internal/service` — бизнес-логика
- `internal/middleware` — auth, CORS

### 2.2 Интеграционные тесты (расширить `tests/integration/cabinet_test.go`)

- Управление тарифами и подписками через Cabinet API
- Связка Cabinet ↔ Billing (баланс, история, подписка)

---

## Этап 3: API Gateway

### 3.1 Unit-тесты `internal/handler`

- Маршрутизация `/v1/recognize`
- Проксирование на Orchestrator
- Проверка API-ключей (через middleware уже частично покрыто)
- Коды ответов: 401, 402, 413, 415, 429

### 3.2 E2E сквозной сценарий

```
1. Регистрация через Cabinet
2. Логин → session token
3. Создание API ключа
4. Пополнение баланса
5. Вызов /v1/recognize с API-ключом
6. Проверка списания через /v1/billing/accounts/{id}/balance
```

---

## Этап 4: Orchestrator & OCR

- Моки OCR-провайдеров (Yandex Vision, VK Vision)
- Тесты нормализации с edge cases
- Fallback-логика при 5xx от OCR

---

## Запуск тестов

```bash
# Unit tests
make test
# или
go test ./pkg/... ./services/...

# С покрытием
go test -coverprofile=coverage.out ./pkg/... ./services/...
go tool cover -html=coverage.out

# Integration tests (требуется Docker Compose)
make docker-up
make test-integration

# E2E billing
go test -tags=e2e ./tests/integration/...
```

---

## Критерии приёмки

- [ ] Биллинг: ≥ 75% покрытие (unit + handler)
- [ ] Cabinet: ≥ 60% покрытие
- [ ] API Gateway: ≥ 60% покрытие
- [ ] E2E сценарий биллинга проходит стабильно
- [ ] E2E сквозной сценарий (регистрация → recognize → списание) проходит
- [ ] Все тесты в CI (GitHub Actions) проходят за < 5 минут
