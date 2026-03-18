# Cabinet Service

Личный кабинет с Web UI для управления аккаунтом организации.

## Структура

```
cabinet/
├── cmd/server/           # Точка входа
│   └── main.go
├── internal/
│   ├── handler/          # HTTP handlers
│   ├── middleware/       # Auth, CORS
│   ├── repository/       # DB layer
│   └── service/          # Business logic
├── pkg/models/           # Data models
├── pages/                # Web UI (HTML/JS/CSS)
│   └── index.html        # Single Page Application
├── docs/                 # Swagger docs
└── Dockerfile
```

## Функциональность

### Backend API

- **Аутентификация**: Регистрация, вход/выход (сессии)
- **API Keys**: Создание, просмотр, отзыв (макс. 10 ключей)
- **Интеграция**: Запрос баланса через Billing Service

### Web UI

Single Page Application на vanilla JS:
- `/` — Личный кабинет (весь функционал на одной странице)
- Автоматическое обновление баланса
- Создание и управление API ключами

## API Endpoints

| Метод | Путь | Auth | Описание |
|-------|------|------|----------|
| POST | `/api/v1/auth/register` | — | Регистрация |
| POST | `/api/v1/auth/login` | — | Вход |
| POST | `/api/v1/auth/logout` | Session | Выход |
| GET | `/api/v1/auth/verify` | Session | Проверка сессии |
| GET | `/api/v1/api-keys` | Session | Список ключей |
| POST | `/api/v1/api-keys` | Session | Создать ключ |
| DELETE | `/api/v1/api-keys/{id}` | Session | Отозвать ключ |

### Формат API Key

```
base64(key_id:secret)
Пример: MTI6UVVoUFZrTktVVmhGVEZOYVIwNVZRa2xRVjBSTFVsbEc=
```

Декодируется в: `12:QUhPVkNKUVhETFNaR05VQklQV0RLUllG`

## База данных

**Таблицы (api_scan):**
- `organizations` — организации
- `users` — пользователи
- `api_keys` — API ключи (bcrypt hash)
- `sessions` — сессии
- `account_events` — история событий

## Тестовые данные

После `make seed` доступен тестовый аккаунт:
- **Email**: `test@example.com`
- **Password**: `password`

## Конфигурация

| Переменная | Описание | Значение по умолчанию |
|------------|----------|----------------------|
| `DATABASE_URL` | PostgreSQL connection string | `postgres://api_scan:api_scan_secret@localhost:5432/api_scan` |
| `PORT` | Порт сервера | `8080` |
| `BILLING_URL` | URL Billing Service | `http://billing:8080` |
| `PAGES_DIR` | Путь к статическим файлам | `./pages` |

## Разработка

```bash
# Запуск
make run-service SERVICE=cabinet

# Или напрямую
go run ./cmd/server/main.go
```

## Swagger

Документация API: http://localhost:8084/swagger/

## Web UI

Откройте http://localhost:8084/ в браузере.
