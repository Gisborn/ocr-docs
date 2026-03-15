# Личный кабинет

Веб-интерфейс для управления аккаунтом организации.

## Структура

```
cabinet/
├── backend/          # API личного кабинета (Go)
│   ├── cmd/
│   ├── internal/
│   └── Dockerfile
└── frontend/         # Веб-интерфейс
    ├── src/
    ├── public/
    └── Dockerfile
```

## Backend

### Функциональность

- Регистрация и аутентификация (email + пароль)
- Управление API-ключами (создание, отзыв)
- Просмотр баланса и тарифа
- История операций (`account_events`)
- Интеграция с ЮКассой (пополнение баланса)

### API Endpoints

| Метод | Путь | Описание |
|-------|------|----------|
| POST | /api/v1/auth/register | Регистрация |
| POST | /api/v1/auth/login | Вход |
| POST | /api/v1/auth/logout | Выход |
| GET | /api/v1/keys | Список API-ключей |
| POST | /api/v1/keys | Создание ключа |
| DELETE | /api/v1/keys/:id | Отзыв ключа |
| GET | /api/v1/balance | Баланс и тариф |
| GET | /api/v1/events | История событий |
| POST | /api/v1/payments | Создание платежа (ЮКасса) |
| POST | /api/v1/payments/webhook | Вебхук ЮКассы |

## Frontend

### Страницы

- `/login` — Вход
- `/register` — Регистрация
- `/dashboard` — Главная (баланс, быстрые действия)
- `/keys` — Управление API-ключами
- `/balance` — Пополнение баланса
- `/history` — История событий
- `/settings` — Настройки организации

## Конфигурация

### Backend

| Переменная | Описание |
|------------|----------|
| `DATABASE_URL` | PostgreSQL |
| `REDIS_URL` | Redis (для сессий) |
| `YOOKASSA_SHOP_ID` | ID магазина ЮКассы |
| `YOOKASSA_SECRET_KEY` | Секретный ключ ЮКассы |
| `YOOKASSA_CALLBACK_URL` | URL для вебхуков |

### Frontend

| Переменная | Описание |
|------------|----------|
| `REACT_APP_API_URL` | URL backend API |
