# Billing Service

Сервис биллинга: резервирование, фиксация и откат транзакций.

## Назначение

- Резервирование средств перед операцией (PENDING)
- Фиксация успешных операций (COMMITTED)
- Откат при ошибках (ROLLBACK)
- Управление балансом и предоплаченными операциями

## Модель транзакций

```
PENDING → COMMITTED (успех)
       → ROLLBACK (техническая ошибка)
```

**Приоритет списания:**
1. Предоплаченные операции (`prepaid_operations_left`)
2. Рублёвый баланс (`balance_rub`)

## API

### POST /reserve

Создание резерва перед операцией.

**Request:**
```json
{
  "org_id": 123,
  "operation_type": "passport_recognition"
}
```

**Response:**
```json
{
  "transaction_id": "txn_...",
  "status": "PENDING",
  "amount_rub": 7.00
}
```

### POST /commit

Фиксация успешной операции.

**Request:**
```json
{
  "transaction_id": "txn_..."
}
```

### POST /rollback

Откат резерва при ошибке.

**Request:**
```json
{
  "transaction_id": "txn_..."
}
```

## Cron-job

Каждые 5 минут переводит `PENDING`-транзакции старше 2 минут в `ROLLBACK`.

## Конфигурация

| Переменная | Описание |
|------------|----------|
| `DATABASE_URL` | PostgreSQL connection string |
| `RESERVE_TIMEOUT` | Таймаут резерва (2m) |
| `CRON_INTERVAL` | Интервал cleanup (5m) |

## Разработка

```bash
go test ./...
```
