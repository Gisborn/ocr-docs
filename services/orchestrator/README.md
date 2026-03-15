# Core Orchestrator

Основной сервис обработки запросов на распознавание паспортов.

## Назначение

- Приём изображений от API Gateway
- OCR через Yandex Vision (primary) и VK Vision (fallback)
- Нормализация данных (ФИО, даты, серия/номер)
- Возврат структурированного результата с confidence

## Архитектура

```
HTTP Request
    │
    ├──→ OCR Provider (Yandex Vision)
    │    └── confidence < порога → Fallback → VK Vision
    │
    └──→ Нормализатор
         └──→ JSON Response + confidences
```

## Зависимости

- `pkg/ocr` — интерфейс OCR-провайдеров
- `pkg/cache` — кэширование (для идемпотентности)
- Billing Service — резервирование и фиксация транзакций

## Конфигурация

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `OCR_PRIMARY_PROVIDER` | Primary OCR (yandex/vk) | yandex |
| `OCR_FALLBACK_PROVIDER` | Fallback OCR (yandex/vk/mock) | vk |
| `OCR_CONFIDENCE_THRESHOLD` | Порог confidence | 0.80 |
| `CIRCUIT_BREAKER_FAILURE_THRESHOLD` | Порог срабатывания CB | 5 |
| `CIRCUIT_BREAKER_TIMEOUT` | Таймаут CB | 60s |
| `YANDEX_VISION_TOKEN` | Токен Yandex Vision (из Lockbox) | - |
| `VK_VISION_TOKEN` | Токен VK Vision (из Lockbox) | - |

## API

### POST /v1/recognize (internal)

Внутренний эндпоинт для вызова из API Gateway.

**Request:** multipart/form-data с полем `file`

**Response:**
```json
{
  "request_id": "req_...",
  "last_name": "Иванов",
  "first_name": "Иван",
  "middle_name": "Иванович",
  "birth_date": "01.01.1990",
  "series": "4510",
  "number": "123456",
  "issue_date": "01.01.2010",
  "issued_by": "...",
  "division_code": "770-001",
  "registration_address": null,
  "confidences": {
    "last_name": 0.999,
    "first_name": 0.998,
    ...
  }
}
```

## Разработка

```bash
# Запуск локально
go run ./cmd/server

# Сборка Docker образа
docker build -t api-scan/orchestrator:latest .

# Тесты
go test ./...
```

## Структура

```
orchestrator/
├── cmd/
│   └── server/           # Точка входа
├── internal/
│   ├── handler/          # HTTP handlers
│   ├── service/          # Бизнес-логика
│   └── config/           # Конфигурация
└── Dockerfile
```
