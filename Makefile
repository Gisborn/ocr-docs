# API-Scan Makefile

.PHONY: help build test clean docker-up docker-down migrate

# Default target
help:
	@echo "API-Scan Makefile commands:"
	@echo "  make build        - Build all services"
	@echo "  make test         - Run all tests"
	@echo "  make docker-up    - Start all services with Docker Compose"
	@echo "  make docker-down  - Stop all services"
	@echo "  make migrate      - Run database migrations"
	@echo "  make swagger      - Generate Swagger documentation"
	@echo "  make clean        - Clean build artifacts"

# Build all services
build:
	@echo "Building services..."
	@go build -o bin/billing ./services/billing/cmd/server
	@go build -o bin/billing-webhook ./services/billing-webhook-yookassa/cmd/server
	@go build -o bin/api-gateway ./services/api-gateway/cmd/server
	@go build -o bin/orchestrator ./services/orchestrator/cmd/server
	@go build -o bin/cabinet ./services/cabinet/cmd/server
	@echo "Build complete. Binaries in bin/"

# Run all tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -cover ./...

# Start infrastructure and services
docker-up:
	@echo "Starting infrastructure..."
	@docker-compose up -d postgres postgres-billing redis
	@echo "Waiting for databases to be ready..."
	@sleep 5
	@echo "Starting services..."
	@docker-compose --profile billing --profile gateway --profile cabinet up -d

# Stop all services
docker-down:
	@echo "Stopping all services..."
	@docker-compose --profile billing --profile gateway --profile cabinet down

# Run migrations
migrate:
	@echo "Running main database migrations..."
	@cd migrations/main && goose up
	@echo "Running billing database migrations..."
	@cd ../billing && goose up

# Rollback migrations
migrate-down:
	@echo "Rolling back main database..."
	@cd migrations/main && goose down
	@echo "Rolling back billing database..."
	@cd ../billing && goose down

# Generate Swagger documentation
swagger:
	@echo "Generating Swagger for Billing Service..."
	@cd services/billing && ~/go/bin/swag init -g cmd/server/main.go
	@echo "Generating Swagger for API Gateway..."
	@cd services/api-gateway && ~/go/bin/swag init -g cmd/server/main.go
	@echo "Generating Swagger for Cabinet..."
	@cd services/cabinet && ~/go/bin/swag init -g cmd/server/main.go
	@echo "Swagger documentation generated."

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@go clean

# Development mode - start infrastructure only
dev-infra:
	@docker-compose up -d postgres postgres-billing redis

# Run specific service (usage: make run-service SERVICE=billing)
run-service:
	@if [ -z "$(SERVICE)" ]; then \
		echo "Usage: make run-service SERVICE=<service-name>"; \
		echo "Available services: billing, api-gateway, orchestrator, cabinet"; \
		exit 1; \
	fi
	@go run ./services/$(SERVICE)/cmd/server/main.go

# Format code
fmt:
	@echo "Formatting code..."
	@gofmt -w .

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run ./...
