# Миграции базы данных

Используем [goose](https://github.com/pressly/goose) для управления миграциями.

## Установка goose

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

## Команды

```bash
# Применить все миграции
goose up

# Откатить последнюю миграцию
goose down

# Статус
goose status

# Создать новую миграцию
goose create add_users_table sql
```

## Структура

```
migrations/
├── 001_initial_schema.sql     # Начальная схема
├── 002_add_indexes.sql        # Индексы
└── README.md                  # Этот файл
```

## Конвенции

- Все миграции должны быть reversible (иметь `UP` и `DOWN`)
- Использовать транзакции для атомарности
- Не удалять колонки с данными — только добавлять новые
- Фиксировать схему в `api-scan-architecture.md`

## Переменные окружения

```bash
export GOOSE_DRIVER=postgres
export GOOSE_DBSTRING="postgres://api_scan:api_scan_secret@localhost:5432/api_scan"
```

Или использовать `DATABASE_URL`:

```bash
goose postgres "$DATABASE_URL" up
```
