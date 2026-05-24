# Общие пакеты (pkg/)

Этот каталог содержит общие интерфейсы и реализации, используемые всеми сервисами.

## Принципы

- **Интерфейсы над имплементациями** — код сервисов зависит от интерфейсов, не от конкретных реализаций
- **Dependency Injection** — конкретные реализации задаются при инициализации сервиса
- **Vendor-agnostic** — легкая замена провайдеров без изменения бизнес-логики

## Структура

### pkg/ocr

Интерфейс OCR-провайдеров.

```go
package ocr

type Provider interface {
    Recognize(ctx context.Context, image []byte) (*Result, error)
}

type Result struct {
    RawText    string
    Confidence float64
    Fields     map[string]Field
}
```

**Реализации:**
- `YandexVision` — Yandex Vision API (primary)
- `VKVision` — VK Vision API (fallback)
- `Mock` — мок для тестирования

### pkg/cache

Интерфейс кэширования.

```go
package cache

type Cache interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
}
```

**Реализации:**
- `Redis` — Yandex Memory Store (Redis)
- `NoopCache` — no-op реализация (fallback при недоступности Redis)

### pkg/normalizer

Нормализация результатов OCR (ФИО, даты, серия/номер паспорта, код подразделения).

### pkg/testdb

Хелперы для SQL-тестов с PostgreSQL:
- Подключение к БД через `TEST_DATABASE_URL`
- Применение goose-миграций
- Очистка таблиц (`TRUNCATE CASCADE`)
- Автоматический `t.Skip()` если БД недоступна

```go
func TestRepo(t *testing.T) {
    pool := testdb.MustPool(t, testdb.DefaultMainURL())
    testdb.ApplyMigrations(t, pool, "../../../../migrations/main")
    testdb.Cleanup(t, pool, "organizations", "users")
    // ...
}
```

### pkg/queue

Интерфейс очереди (для v2 — асинхронная модель).

```go
package queue

type Queue interface {
    Publish(ctx context.Context, message []byte) error
    Consume(ctx context.Context, handler Handler) error
}
```

**Реализации:**
- `YMQ` — Yandex Message Queue
- `Mock` — in-memory реализация для тестов

### pkg/storage

Интерфейсы работы с БД.

```go
package storage

type DB interface {
    QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
    ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
    BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}
```

## Использование в сервисах

```go
// Пример: инициализация orchestrator
func main() {
    // Выбор реализации через env
    ocrProvider := ocr.NewYandexVision(cfg.YandexToken)
    if cfg.OCRProvider == "vk" {
        ocrProvider = ocr.NewVKVision(cfg.VKToken)
    }
    
    cache := cache.NewRedis(cfg.RedisURL)
    if cfg.CacheType == "noop" {
        cache = cache.NewNoopCache()
    }
    
    service := orchestrator.NewService(ocrProvider, cache)
    // ...
}
```
