# Billing Service

Сервис биллинга: управление счетами, резервирование средств, подписки.

## Назначение

- Управление балансом (real + prepaid)
- Двухфазная фиксация транзакций (Reserve → Commit/Rollback)
- Управление подписками
- Интеграция с платёжными системами (YooKassa)

## Модель транзакций

```
RESERVE (PENDING) → COMMIT (успех)
                  → ROLLBACK (ошибка/таймаут)
```

**Расчёт баланса:**
```
Balance = Snapshot Balance + Σ Events Since Snapshot - Active Reservations
```

## API Endpoints

### Accounts

| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/accounts` | Создать счёт |
| GET | `/accounts/{id}/balance` | Получить баланс |
| POST | `/accounts/{id}/topup` | Пополнить баланс (internal) |
| GET | `/accounts/{id}/events` | История биллинг-событий |

### Transactions (Two-Phase Commit)

| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/accounts/{id}/reserve` | Зарезервировать средства |
| POST | `/transactions/{id}/commit` | Зафиксировать транзакцию |
| POST | `/transactions/{id}/rollback` | Откатить транзакцию |

**Reserve Request:**
```json
{
  "amount_rub": 7.00,
  "service_id": "passport_rf"
}
```

**Reserve Response:**
```json
{
  "request_id": "req_abc123",
  "status": "pending",
  "expires_at": "2026-03-18T16:30:00Z"
}
```

### Subscriptions

| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/accounts/{id}/subscriptions` | Создать подписку |
| POST | `/accounts/{id}/subscriptions/upgrade` | Апгрейд подписки |
| POST | `/accounts/{id}/subscriptions/cancel` | Отмена подписки |

### Payments

| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/accounts/{id}/payments` | Создать платёж |
| GET | `/payments/{id}` | Статус платежа |
| GET | `/accounts/{id}/payments` | Список платежей |

## База данных

**Таблицы:**
- `accounts` — счета организаций
- `balance_snapshots` — снапшоты баланса
- `billing_events` — события (immutable)
- `reservations` — активные резервы
- `subscriptions` — подписки
- `payment_orders` — заказы на оплату

## Конфигурация

| Переменная | Описание | Значение по умолчанию |
|------------|----------|----------------------|
| `DATABASE_URL` | PostgreSQL connection string | — |
| `PORT` | Порт сервера | `8080` |
| `YOOKASSA_SECRET_KEY` | Секретный ключ ЮКассы | — |
| `YOOKASSA_SHOP_ID` | ID магазина ЮКассы | — |

## Разработка

```bash
# Запуск тестов
go test ./...

# Запуск сервиса
go run ./cmd/server/main.go
```

## Swagger

Документация API: http://localhost:8081/swagger/
