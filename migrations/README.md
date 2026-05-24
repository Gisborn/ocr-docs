# Миграции базы данных

Используем [goose](https://github.com/pressly/goose) для управления миграциями.

## Установка goose

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

## Структура

```
migrations/
├── main/                   # api_scan — основная БД (организации, пользователи, сессии, API ключи)
│   ├── 001_initial_schema.sql
│   ├── 004_sessions_add_billing_account.sql
│   ├── 005_add_accepted_terms.sql
│   └── 006_mock_payments.sql
└── billing/                # billing_db — биллинг (счета, транзакции, подписки, тарифы)
    ├── 001_initial_schema.sql
    └── 002_mock_payments.sql
```

## Команды

```bash
# Применить все миграции main БД
cd migrations/main && goose up

# Применить все миграции billing БД
cd migrations/billing && goose up

# Откатить последнюю миграцию
goose down

# Статус
goose status

# Создать новую миграцию
goose create add_users_table sql
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
# Main DB
goose postgres "postgres://api_scan:api_scan_secret@localhost:5432/api_scan" up

# Billing DB
goose postgres "postgres://billing:billing_secret@localhost:5433/billing_db" up
```
