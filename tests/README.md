# Тесты

## Структура

```
tests/
├── integration/          # E2E интеграционные тесты (с реальными сервисами)
│   ├── billing_flow_test.go   # Полный флоу: register → login → API key → topup → subscription → reserve → commit → history
│   └── cabinet_test.go        # Тесты Cabinet + API Gateway
├── fixtures/             # Тестовые данные (изображения паспортов)
└── mocks/                # Моки для OCR провайдеров
```

## Типы тестов

### 1. Unit Tests (с моками)

Тестируют отдельные компоненты с мок-репозиториями.

```bash
# Запуск (SQL тесты пропускаются автоматически, если БД недоступна)
make check

# Или
export PATH="/c/ALEX/go/bin:$PATH"
go test -count=1 ./pkg/... ./services/...

# Полная проверка с Docker БД + линтер
make pre-push
```

**Покрытие:**
- `services/billing/internal/service` — billing logic
- `services/billing/internal/handler` — HTTP handlers
- `services/api-gateway/internal/handler` — routing, auth
- `services/api-gateway/internal/middleware` — rate limiting, CORS
- `services/cabinet/internal/service` — auth, API keys, payments, subscriptions
- `services/cabinet/internal/handler` — HTTP handlers
- `services/cabinet/internal/middleware` — session auth
- `services/orchestrator/internal/service` — OCR orchestration, circuit breaker
- `services/billing-webhook-yookassa` — webhook handling

### 2. SQL Repository Tests (с реальной PostgreSQL)

Тестируют PostgreSQL-репозитории всех сервисов.

```bash
# 1. Поднять PostgreSQL и Redis
docker compose -f infra/docker/docker-compose.test.yml up -d

# 2. Запустить SQL тесты (миграции применяются автоматически)
export TEST_DATABASE_URL=postgres://api_scan:api_scan_secret@localhost:15432/api_scan
export TEST_BILLING_DATABASE_URL=postgres://billing:billing_secret@localhost:15433/billing_db
go test -count=1 ./services/api-gateway/internal/repository/...
go test -count=1 ./services/billing/internal/repository/...
go test -count=1 ./services/billing-webhook-yookassa/internal/repository/...
go test -count=1 ./services/cabinet/internal/repository/...
```

**Покрытые репозитории:**
- `services/api-gateway/internal/repository` — API keys, organizations
- `services/billing-webhook-yookassa/internal/repository` — payment orders, billing events
- `services/billing/internal/repository` — accounts, balance, reservations, subscriptions, tariffs, payments
- `services/cabinet/internal/repository` — organizations, users, API keys, sessions, account events

### 3. Integration / E2E Tests (с реальными сервисами)

Тестируют полные сценарии с реальными базами данных и сервисами.

```bash
# 1. Запустить все сервисы
make docker-up

# 2. Запустить интеграционные тесты
make test-integration
```

**Сценарии:**
- Регистрация → Логин → Создание API ключа → Проверка баланса
- Полный billing flow: topup → subscription → reserve → commit → history
- Сессии: создание, валидация, выход
- API ключи: формат, валидация

**Требования:**
- PostgreSQL на портах 15432, 15433
- Redis на порту 16379
- Cabinet Service на порту 8084
- API Gateway на порту 8080

### 4. Quick Test Script

Быстрая проверка всех сервисов:

```bash
./scripts/quick-test.sh
```

Проверяет:
1. Health всех сервисов
2. Логин в Cabinet
3. Создание API ключа
4. Проверку баланса

## Написание тестов

### Unit Test с моком

```go
func TestService_Method(t *testing.T) {
    repo := NewMockRepository()
    svc := NewService(repo)
    
    // Test
    result, err := svc.Method(ctx, req)
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}
```

### SQL Repository Test

```go
func TestPostgresRepository_Method(t *testing.T) {
    // SetupTestDB создаёт эфемерную схему, применяет миграции,
    // сбрасывает SERIAL-последовательности и регистрирует t.Cleanup
    pool := testdb.SetupTestDB(t, "postgres://...", "../../../../migrations/main")
    
    repo := repository.NewPostgresRepository(pool)
    // Test CRUD operations against real PostgreSQL
}
```

### Integration Test

```go
func TestFlow(t *testing.T) {
    // Подключаемся к реальной БД
    pool, _ := pgxpool.New(ctx, databaseURL)
    
    // Тестируем полный сценарий
    // 1. Создаём организацию
    // 2. Логинимся
    // 3. Создаём API ключ
    // 4. Проверяем что ключ работает
}
```

## CI/CD

В pipeline (`.github/workflows/ci.yml`) запускаются:
1. **Lint** — `golangci-lint` (Go 1.24)
2. **Unit tests** — `go test -race` с эфемерными PostgreSQL-схемами
3. **Integration tests** — E2E с PostgreSQL + Redis в Docker, миграции `goose@v3.24.0`
4. **Build images** — проверка сборки всех Docker образов

### Pre-push hook

Локальный `.git/hooks/pre-push` блокирует push, если:
- `go build` падает
- `go test` падает
- `golangci-lint` найдёт ошибки (если установлен)

```bash
make pre-push   # полная проверка с Docker БД
make check      # быстрая проверка без Docker
```
