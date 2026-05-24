# API-Scan Makefile

.PHONY: help build test clean docker-up docker-down migrate

# Default target
help:
	@echo "API-Scan Makefile commands:"
	@echo "  make docker-up    - Start all services (infra + migrate + services)"
	@echo "  make docker-down  - Stop all services"
	@echo "  make health       - Check all service health status"
	@echo "  make seed         - Seed database with test data"
	@echo "  make migrate      - Run database migrations"
	@echo "  make build        - Build all services binaries"
	@echo "  make test         - Run all tests"
	@echo "  make swagger      - Generate Swagger documentation"
	@echo "  make clean        - Clean build artifacts"
	@echo ""
	@echo "See LOCAL_TESTING.md for detailed testing guide"

# Build all services
build:
	@echo "Building services..."
	@go build -o bin/billing ./services/billing/cmd/server
	@go build -o bin/billing-webhook ./services/billing-webhook-yookassa/cmd/server
	@go build -o bin/api-gateway ./services/api-gateway/cmd/server
	@go build -o bin/orchestrator ./services/orchestrator/cmd/server
	@go build -o bin/cabinet ./services/cabinet/cmd/server
	@echo "Build complete. Binaries in bin/"

# Run all tests (excluding root and scripts)
test:
	@echo "Running tests..."
	@go test -v ./pkg/... ./services/... ./tests/...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -cover ./...

# Run integration tests (requires running services)
test-integration:
	@echo "Running integration tests..."
	@echo "Make sure services are running: make docker-up"
	@go test -v ./tests/integration/...

# Start infrastructure and services
docker-up:
	@echo "Starting infrastructure..."
	@docker-compose up -d postgres postgres-billing redis
	@echo "Waiting for databases to be ready..."
	@sleep 10
	@echo "Running migrations..."
	@which goose > /dev/null 2>&1 && (cd migrations/main && goose up) || echo "  goose not installed, skipping migrations (DB will use init scripts)"
	@which goose > /dev/null 2>&1 && (cd migrations/billing && goose up) || echo "  goose not installed, skipping billing migrations"
	@echo "Starting services..."
	@docker-compose --profile billing --profile gateway --profile cabinet --profile orchestrator up -d
	@echo ""
	@echo "✓ Services started! Test data available:"
	@echo "  Email: test@example.com"
	@echo "  Password: password"
	@echo ""
	@echo "Quick test:"
	@echo "  curl -X POST http://localhost:8084/api/v1/auth/login \\"
	@echo "    -H 'Content-Type: application/json' \\"
	@echo "    -d '{\"email\":\"test@example.com\",\"password\":\"password\"}'"

# Stop all services
docker-down:
	@echo "Stopping all services..."
	@docker-compose --profile billing --profile gateway --profile cabinet --profile orchestrator down

# Check health of all services
health:
	@echo "Checking service health..."
	@echo "API Gateway:     $$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/health 2>/dev/null || echo 'DOWN')"
	@echo "Billing:         $$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8081/health 2>/dev/null || echo 'DOWN')"
	@echo "Billing Webhook: $$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8082/health 2>/dev/null || echo 'DOWN')"
	@echo "Orchestrator:    $$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8083/health 2>/dev/null || echo 'DOWN')"
	@echo "Cabinet:         $$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8084/health 2>/dev/null || echo 'DOWN')"
	@echo "PostgreSQL:      $$(docker exec api-scan-postgres pg_isready -U api_scan >/dev/null 2>&1 && echo 'UP' || echo 'DOWN')"
	@echo "PostgreSQL-Bill: $$(docker exec api-scan-postgres-billing pg_isready -U billing >/dev/null 2>&1 && echo 'UP' || echo 'DOWN')"
	@echo "Redis:           $$(docker exec api-scan-redis redis-cli ping 2>/dev/null || echo 'DOWN')"

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

# Seed database with test data
seed:
	@echo "Seeding main database..."
	@docker exec -i api-scan-postgres psql -U api_scan -d api_scan < scripts/seed.sql 2>/dev/null || \
		psql "postgres://api_scan:api_scan_secret@localhost:5432/api_scan" < scripts/seed.sql
	@echo "Seeding billing database..."
	@docker exec -i api-scan-postgres-billing psql -U billing -d billing_db < scripts/seed.sql 2>/dev/null || \
		psql "postgres://billing:billing_secret@localhost:5433/billing_db" < scripts/seed.sql
	@echo "✓ Test data seeded"

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

# Pre-push checks: everything CI does, locally
pre-push:
	@echo "=== Pre-push checks ==="
	@echo "→ Starting test databases..."
	@docker compose -f infra/docker/docker-compose.test.yml up -d postgres postgres-billing redis
	@echo "→ Waiting for databases..."
	@sleep 5
	@echo "→ Building..."
	@go build ./pkg/... ./services/...
	@echo "→ Running tests..."
	@go test -race -count=1 ./pkg/... ./services/...
	@echo "→ Running linter..."
	@golangci-lint run --timeout=5m ./...
	@echo "→ Stopping test databases..."
	@docker compose -f infra/docker/docker-compose.test.yml down
	@echo "✅ Pre-push checks passed"

# Quick local check (no DB required, skips SQL tests)
check:
	@echo "=== Quick check ==="
	@go build ./pkg/... ./services/...
	@go test -short -count=1 ./pkg/... ./services/...
	@golangci-lint run --timeout=5m ./...
	@echo "✅ Quick check passed"
