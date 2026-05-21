# pkg/ocr

Интерфейсы и реализации OCR-провайдеров.

## Структура

```
ocr/
├── provider.go     # Интерфейс Provider и общие типы (Result, Field, ProviderError)
├── model.go        # Типы моделей документов (passport, driver-license-front и т.д.)
├── parser.go       # Извлечение полей паспорта из raw text (generic OCR fallback)
├── yandex_v2.go    # Yandex Vision OCR v2 — primary, поддержка моделей (passport, page и др.)
├── yandex.go       # Yandex Vision OCR v1 — legacy generic OCR (сохранён для совместимости)
├── vk.go           # VK Vision — внешний fallback
└── mock.go         # Моки для тестирования
```

## Архитектура OCR

```
┌─────────────────┐     ┌─────────────────────┐     ┌─────────────────┐
│  YandexVisionV2 │────→│  passport model     │────→│  entities       │
│  (primary)      │     │  (structured output)│     │  готовые поля   │
└─────────────────┘     └─────────────────────┘     └─────────────────┘
         │
         │ insufficient fields
         ▼
┌─────────────────┐     ┌─────────────────────┐     ┌─────────────────┐
│  YandexVisionV2 │────→│  page model         │────→│  ExtractPassport│
│  (internal fb)  │     │  (generic OCR)      │     │  Fields()       │
└─────────────────┘     └─────────────────────┘     └─────────────────┘
         │
         │ error
         ▼
┌─────────────────┐     ┌─────────────────────┐     ┌─────────────────┐
│  VKVision       │────→│  TEXT_DETECTION     │────→│  ExtractPassport│
│  (external fb)  │     │  (generic OCR)      │     │  Fields()       │
└─────────────────┘     └─────────────────────┘     └─────────────────┘
```

## Использование

### Yandex Vision v2 (рекомендуется)

```go
// Создание провайдера с моделью распознавания
provider := ocr.NewYandexVisionV2(apiKey, ocr.ModelPassportRF)

// Распознавание с автоматическим fallback:
// 1. Сначала пробуется structured model (passport)
// 2. Если полей недостаточно — fallback на generic page model + парсинг
result, err := provider.Recognize(ctx, imageBytes)
```

### Yandex Vision v1 (legacy)

```go
// Требуется folderID
yandex := ocr.NewYandexVision(apiKey, folderID)
result, err := yandex.Recognize(ctx, imageBytes)
```

### Поддерживаемые модели (Yandex Vision v2)

**Шаблонные документы (structured output):**
- `passport` — паспорт РФ
- `driver-license-front` — водительское удостоверение (лицевая)
- `driver-license-back` — водительское удостоверение (оборотная)
- `vehicle-registration-front` — СТС (лицевая)
- `vehicle-registration-back` — СТС (оборотная)
- `license-plates` — номера автомобилей

**Generic OCR:**
- `page` — обычный текст (default fallback)
- `page-column-sort` — многоколонный текст
- `handwritten` — рукописный текст
- `table` — таблицы
- `markdown` — markdown-формат
- `math-markdown` — математические формулы

### Конфигурация через env

```bash
# Обязательно
YANDEX_VISION_API_KEY=AQVN...

# Опционально (по умолчанию: passport)
YANDEX_VISION_USE_V2=true
YANDEX_VISION_MODEL=passport

# Для legacy v1 (если USE_V2=false)
YANDEX_FOLDER_ID=b1g34rjua1gpova54uc2
```

## Тестирование

```go
// Мок с фиксированным результатом
mock := ocr.NewMockProvider()

// Мок с вероятностью ошибки
errorMock := ocr.NewMockProviderWithFailure("fail", 0.5)
```
