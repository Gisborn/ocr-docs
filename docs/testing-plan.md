# План тестирования API-Scan

> Статус: В работе  
> Приоритет: SQL Repository → Orchestrator handler → API Gateway middleware  
> Цель: ≥ 70% покрытие по критичным сервисам

---

## Текущее состояние

| Компонент | Покрытие* | Типы тестов | Приоритет |
|-----------|-----------|-------------|-----------|
| `pkg/normalizer` | 49.8% | Unit | Средний |
| `pkg/ocr` | 46.6% | Unit | Средний |
| `services/api-gateway/internal/handler` | 90.1% | Unit | ✅ Готово |
| `services/api-gateway/internal/middleware` | 45.7% | Unit | Средний |
| `services/api-gateway/internal/repository` | SQL** | Integration | ✅ Готово |
| `services/billing/internal/service` | 68.3% | Unit | ✅ Готово |
| `services/billing/internal/handler` | 59.7% | Unit | Средний |
| `services/billing/internal/repository` | SQL** | Integration | ✅ Готово |
| `services/billing-webhook-yookassa/internal/handler` | 74.2% | Unit | ✅ Готово |
| `services/billing-webhook-yookassa/internal/repository` | SQL** | Integration | ✅ Готово |
| `services/cabinet/internal/handler` | 39.1% | Unit | Средний |
| `services/cabinet/internal/middleware` | 79.3% | Unit | ✅ Готово |
| `services/cabinet/internal/service` | 74.7% | Unit | ✅ Готово |
| `services/cabinet/internal/repository` | SQL** | Integration | ✅ Готово |
| `services/orchestrator/internal/handler` | 43.6% | Unit | Средний |
| `services/orchestrator/internal/service` | 89.7% | Unit | ✅ Готово |

\* Покрытие замеряется локально без SQL-тестов (БД недоступна).  
\** SQL-тесты написаны, запускаются в CI с PostgreSQL.

---

## SQL Repository тесты

Все 4 репозитория покрыты интеграционными тестами с реальной PostgreSQL:

| Репозиторий | БД | Таблицы | Методы |
|-------------|-----|---------|--------|
| `api-gateway/internal/repository` | main | `organizations`, `api_keys` | GetAPIKeyByID, GetOrganization, UpdateAPIKeyLastUsed |
| `billing-webhook-yookassa/internal/repository` | billing | `accounts`, `payment_orders`, `billing_events` | GetPaymentOrderByYookassaID, UpdatePaymentOrder, CreateBillingEvent, GetExpiredPendingPayments |
| `billing/internal/repository` | billing | `accounts`, `balance_snapshots`, `reservations`, `billing_events`, `subscriptions`, `tariffs`, `tariff_versions`, `tariff_service_prices`, `payment_orders` | CreateAccount, GetAccount, GetAccountBalance, UpdateBalanceSnapshot, CreateReservation, GetReservation, DeleteReservation, GetActiveReservations, DeleteExpiredReservations, CreateBillingEvent, GetBillingEventsSince, CreateSubscription, GetActiveSubscription, UpdateSubscription, GetSubscription, GetTariff, GetTariffVersion, GetTariffVersionByCode, GetServicePrice, CreatePaymentOrder, GetPaymentOrder, UpdatePaymentOrder, GetExpiredPendingPayments, BeginTx |
| `cabinet/internal/repository` | main | `organizations`, `users`, `api_keys`, `sessions`, `account_events` | CreateOrganization, GetOrganizationByEmail, GetOrganizationByID, UpdateOrganization, SetBillingAccountID, CreateUser, GetUserByEmail, GetUserByID, UpdateUser, UpdateLastLogin, CreateAPIKey, GetAPIKeyByID, ListAPIKeys, RevokeAPIKey, CountActiveAPIKeys, UpdateAPIKeyHash, CreateSession, GetSessionByToken, DeleteSession, CreateAccountEvent, ListAccountEvents |

### Запуск SQL тестов локально

```bash
# Поднять PostgreSQL
docker compose -f infra/docker/docker-compose.test.yml up -d postgres postgres-billing

# Запустить миграции
cd migrations/main && goose postgres "postgres://api_scan:api_scan_secret@localhost:5432/api_scan?sslmode=disable" up
cd migrations/billing && goose postgres "postgres://billing:billing_secret@localhost:5433/billing_db?sslmode=disable" up

# Запустить тесты
export TEST_DATABASE_URL=postgres://api_scan:api_scan_secret@localhost:5432/api_scan?sslmode=disable
export TEST_BILLING_DATABASE_URL=postgres://billing:billing_secret@localhost:5433/billing_db?sslmode=disable
go test ./services/...
```

### Архитектура SQL тестов

- `pkg/testdb/helper.go` — подключение к PostgreSQL, применение миграций (goose-формат), очистка таблиц
- Тесты пропускаются (`t.Skip`) если БД недоступна — не ломают `go test ./...` без инфраструктуры
- Миграции применяются перед каждым тестом, таблицы очищаются через `TRUNCATE CASCADE`

---

## Запуск тестов

```bash
# Unit tests (без БД — SQL тесты пропускаются)
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

- [x] Биллинг: service ≥ 65%, handler ≥ 50%
- [x] Cabinet: service ≥ 70%, middleware ≥ 75%
- [x] API Gateway: handler ≥ 80%
- [x] SQL Repository тесты для всех сервисов
- [ ] Orchestrator handler ≥ 60%
- [ ] API Gateway middleware ≥ 60%
- [ ] E2E сценарий биллинга проходит стабильно
- [ ] E2E сквозной сценарий (регистрация → recognize → списание) проходит
- [x] Все тесты в CI (GitHub Actions) проходят за < 5 минут
