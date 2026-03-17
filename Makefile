# api-scan Makefile

.PHONY: help build test lint migrate-up migrate-down

# Переменные для БД
DB_HOST ?= localhost
DB_PORT ?= 5432
DB_USER ?= api_scan
DB_PASSWORD ?= api_scan_secret
DB_NAME ?= api_scan
DATABASE_URL ?= postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable

# Default target
help: ## Показать справку
	@echo "Доступные команды:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# === Development ===

dev-up: ## Запустить локальные зависимости (PostgreSQL, Redis) и применить миграции
	docker compose up -d
	@echo "Ожидание запуска PostgreSQL..."
	@sleep 3
	@echo "Применение миграций..."
	@make migrate-up
	@echo "Проверка подключения..."
	@make db-check

dev-down: ## Остановить локальные зависимости
	docker compose down

dev-logs: ## Показать логи контейнеров
	docker compose logs -f

dev-clean: ## Очистить данные БД и volumes
	docker compose down -v
	rm -rf postgres_data/

# === Database ===

db-check: ## Проверить подключение к PostgreSQL
	@./scripts/test-db.sh

migrate-up: ## Применить все миграции
	@echo "Применение миграций..."
	@cd migrations && goose postgres "$(DATABASE_URL)" up

migrate-down: ## Откатить последнюю миграцию
	@cd migrations && goose postgres "$(DATABASE_URL)" down

migrate-status: ## Статус миграций
	@cd migrations && goose postgres "$(DATABASE_URL)" status

migrate-reset: ## Откатить все миграции и применить заново
	@cd migrations && goose postgres "$(DATABASE_URL)" reset
	@cd migrations && goose postgres "$(DATABASE_URL)" up

migrate-create: ## Создать новую миграцию (make migrate-create name=add_users_table)
	@cd migrations && goose create $(name) sql

# === Build ===

build: ## Собрать все сервисы
	@echo "Сборка orchestrator..."
	@docker build -t api-scan/orchestrator:latest ./services/orchestrator
	@echo "Сборка billing..."
	@docker build -t api-scan/billing:latest ./services/billing
	@echo "Сборка cabinet-backend..."
	@docker build -t api-scan/cabinet-backend:latest ./services/cabinet/backend
	@echo "Сборка завершена"

# === Testing ===

test: ## Запустить все тесты
	@go test ./pkg/... -v

test-integration: ## Запустить интеграционные тесты (требуется БД)
	@go test ./tests/integration/... -v

test-e2e: ## Запустить e2e тесты
	@go test ./tests/e2e/... -v

# === Linting ===

lint: ## Запустить линтер
	@golangci-lint run ./...

fmt: ## Форматировать код
	@go fmt ./...

# === Terraform ===

terraform-init: ## Инициализировать Terraform
	@cd infra/terraform && terraform init

terraform-plan: ## План изменений инфраструктуры
	@cd infra/terraform && terraform plan

terraform-apply: ## Применить изменения инфраструктуры
	@cd infra/terraform && terraform apply

# === CI/CD ===

ci-test: ## Команда для CI (тесты + линтер)
	@make lint
	@make test

# === Clean ===

clean: ## Очистить артефакты сборки
	@rm -rf bin/ dist/
	@docker system prune -f
