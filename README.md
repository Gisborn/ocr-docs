# api-scan

Облачный API-сервис для распознавания паспортов РФ.

## Миссия

Исключить ручной ввод паспортных данных в B2B-секторе за счёт облачного OCR-распознавания и бесшовной интеграции с 1С.

## Структура репозитория

```
api-scan/
├── docs/                   # Документация и планирование
├── services/
│   ├── orchestrator/       # Core Orchestrator (OCR + нормализация)
│   ├── billing/            # Billing Service (резервирование, списание)
│   └── cabinet/            # Личный кабинет
│       ├── backend/        # API личного кабинета
│       └── frontend/       # Веб-интерфейс
├── pkg/
│   ├── ocr/                # Интерфейс OcrProvider + адаптеры
│   ├── queue/              # Интерфейс Queue + адаптеры (для v2)
│   ├── cache/              # Интерфейс Cache + адаптеры
│   └── storage/            # Общие типы и интерфейсы БД
├── infra/
│   └── terraform/          # Инфраструктура (staging + production)
├── migrations/             # SQL-миграции БД (goose)
├── tests/
│   ├── integration/        # Интеграционные тесты
│   └── e2e/                # End-to-end тесты
└── scripts/                # Вспомогательные скрипты
```

## Документация

- [Архитектура](docs/api-scan-architecture.md) — описание системы, нефункциональные требования, схема данных
- [План реализации MVP](docs/api-scan-plan.md) — поэтапный план разработки
- [AGENTS.md](AGENTS.md) — руководство для AI-агентов

## Технологический стек

- **Инфраструктура:** Yandex Cloud
- **Вычисления:** Yandex Serverless Container
- **База данных:** Yandex Managed PostgreSQL
- **Кэш:** Yandex Memory Store (Redis)
- **OCR:** Yandex Vision (primary) + VK Vision (fallback)
- **Платёжная система:** ЮКасса (YooKassa)
- **Миграции:** goose

## Начало работы

### Требования

- Go 1.22+
- Docker и Docker Compose
- Terraform 1.7+
- Yandex Cloud CLI (`yc`)

### Локальная разработка

```bash
# Клонирование репозитория
git clone <repo-url>
cd api-scan

# Запуск зависимостей (PostgreSQL, Redis)
docker-compose up -d

# Применение миграций
cd migrations
goose up

# Запуск сервисов
```

## Разработка

### Конвенции

- Весь код в `services/` и `pkg/` — на Go
- Каждый сервис собирается в независимый Docker-образ
- Интерфейсы в `pkg/` абстрагируют внешние зависимости (OCR, кэш, очередь)
- Конкретные реализации задаются через env-переменные (dependency injection)

### CI/CD

- Pull Request → линтер и тесты (обязательно)
- Merge в `main` → автодеплой в `staging`
- Ручное подтверждение → деплой в `production`

## Лицензия

[Указать лицензию]
