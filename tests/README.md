# Тесты

## Структура

```
tests/
├── integration/          # Интеграционные тесты
│   ├── billing_test.go
│   ├── ocr_test.go
│   └── api_test.go
├── e2e/                  # End-to-end тесты
│   └── recognition_flow_test.go
├── fixtures/             # Тестовые данные
│   ├── passports/        # Изображения паспортов
│   └── ocr_responses/    # Примеры ответов OCR
└── mocks/                # Моки внешних сервисов
    └── ocr/
```

## Интеграционные тесты

Тестируют взаимодействие компонентов с реальными зависимостями (PostgreSQL, Redis).

```bash
# Запустить зависимости
docker-compose up -d

# Применить миграции
make migrate-up

# Запустить тесты
go test ./tests/integration/... -v
```

## E2E тесты

Тестируют полные сценарии от API до результата.

```bash
# Запустить все сервисы
make docker-up

# Запустить e2e тесты
go test ./tests/e2e/... -v
```

## Тестовые данные

### Паспорта

- `fixtures/passports/good/` — качественные фото (≥95% точности)
- `fixtures/passports/medium/` — среднее качество
- `fixtures/passports/bad/` — плохое качество (для теста порога confidence)

**Важно:** Не коммитить реальные паспорта! Использовать:
- Сгенерированные тестовые изображения
- Образцы с затёртыми/изменёнными данными
- Публично доступные образцы

## Моки OCR

Директория `tests/mocks/ocr/` содержит конфигурацию Wiremock для эмуляции OCR-провайдеров.

```bash
# Запустить мок
docker-compose up ocr-mock -d

# Мок доступен на http://localhost:8080
```
