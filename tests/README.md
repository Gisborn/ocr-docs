# Тесты

## Структура

```
tests/
├── integration/          # Интеграционные тесты (с реальной БД)
│   └── cabinet_test.go   # Тесты Cabinet + API Gateway
└── mocks/                # Моки для OCR провайдеров
```

## Типы тестов

### 1. Unit Tests (с моками)

Тестируют отдельные компоненты с мок-репозиториями.

```bash
# Запуск
make test

# Или
go test ./services/...
```

**Покрытие:**
- `services/billing/internal/service` — billing logic
- `services/api-gateway/internal/middleware` — auth middleware
- `services/billing-webhook-yookassa` — webhook handling
- `services/orchestrator` — OCR processing

### 2. Integration Tests (с реальной БД)

Тестируют полные сценарии с реальными базами данных.

```bash
# 1. Запустить все сервисы
make docker-up

# 2. Запустить интеграционные тесты
make test-integration
```

**Сценарии:**
- Регистрация → Логин → Создание API ключа → Проверка баланса
- Сессии: создание, валидация, выход
- API ключи: формат, валидация

**Требования:**
- PostgreSQL на портах 5432, 5433
- Redis на порту 6379
- Cabinet Service на порту 8084
- API Gateway на порту 8080

### 3. Quick Test Script

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
    
    // Setup mock
    repo.expectations = ...
    
    // Test
    result, err := svc.Method(ctx, req)
    
    // Assert
    assert.NoError(t, err)
    assert.Equal(t, expected, result)
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

В pipeline должны запускаться:
1. Unit tests (быстро, с моками)
2. Integration tests (требуют Docker)
3. Lint checks
