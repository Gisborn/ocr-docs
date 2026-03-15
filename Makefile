# api-scan Makefile

.PHONY: help build test lint migrate-up migrate-down docker-up docker-down

# Default target
help: ## Показать справку
	@echo "Доступные команды:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Build
build: ## Собрать все сервисы
	@echo "Сборка orchestrator..."
	@docker build -t api-scan/orchestrator:latest ./services/orchestrator
	@echo "Сборка billing..."
	@docker build -t api-scan/billing:latest ./services/billing
	@echo "Сборка cabinet-backend..."
	@docker build -t api-scan/cabinet-backend:latest ./services/cabinet/backend
	@echo "Сборка завершена"

# Testing
test: ## Запустить все тесты
	@go test ./pkg/... ./services/... -v

test-integration: ## Запустить интеграционные тесты
	@go test ./tests/integration/... -v

test-e2e: ## Запустить e2e тесты
	@go test ./tests/e2e/... -v

# Linting
lint: ## Запустить линтер
	@golangci-lint run ./...

fmt: ## Форматировать код
	@go fmt ./...

# Database migrations
migrate-up: ## Применить миграции
	@cd migrations && goose up

migrate-down: ## Откатить последнюю миграцию
	@cd migrations && goose down

migrate-status: ## Статус миграций
	@cd migrations && goose status

migrate-create: ## Создать новую миграцию (использование: make migrate-create name=add_users_table)
	@cd migrations && goose create $(name) sql

# Docker Compose (local development)
docker-up: ## Запустить локальные зависимости (PostgreSQL, Redis)
	@docker-compose up -d

docker-down: ## Остановить локальные зависимости
	@docker-compose down

docker-logs: ## Показать логи контейнеров
	@docker-compose logs -f

# Development
dev-orchestrator: ## Запустить orchestrator локально
	@cd services/orchestrator && go run ./cmd/server

dev-billing: ## Запустить billing локально
	@cd services/billing && go run ./cmd/server

dev-cabinet: ## Запустить cabinet backend локально
	@cd services/cabinet/backend && go run ./cmd/server

# Terraform
terraform-init: ## Инициализировать Terraform
	@cd infra/terraform && terraform init

terraform-plan: ## План изменений инфраструктуры
	@cd infra/terraform && terraform plan

terraform-apply: ## Применить изменения инфраструктуры
	@cd infra/terraform && terraform apply

# CI/CD
ci-test: ## Команда для CI (тесты + линтер)
	@make lint
	@make test

# Clean
clean: ## Очистить артефакты сборки
	@rm -rf bin/ dist/
	@docker-compose down -v
	@docker system prune -f
