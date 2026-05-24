# Contributing to API-Scan

## Development Workflow

### 1. Setup Local Environment

```bash
# Install dependencies
go mod download

# Install swag CLI for documentation generation
go install github.com/swaggo/swag/cmd/swag@latest

# Start infrastructure (databases, redis)
docker-compose up -d postgres postgres-billing redis

# Run migrations
cd migrations/main && goose up
cd ../billing && goose up
```

### 2. Run Services Locally

```bash
# Terminal 1: Billing Service
cd services/billing
go run cmd/server/main.go

# Terminal 2: API Gateway
cd services/api-gateway
go run cmd/server/main.go

# Terminal 3: Orchestrator
cd services/orchestrator
go run cmd/server/main.go

# Terminal 4: Cabinet
cd services/cabinet
go run cmd/server/main.go
```

### 3. Code Style

- **Go**: Standard gofmt, go vet
- **Imports**: Grouped by stdlib, external, internal
- **Comments**: All exported functions must have comments
- **Error handling**: Always check errors, wrap with context

### 4. Testing

```bash
# Run all tests (SQL tests skip automatically if DB is unavailable)
go test ./...

# Run with coverage
go test -cover ./...

# Run specific test
go test -run TestCreatePayment ./services/billing/internal/service/...

# Run SQL repository tests (requires PostgreSQL)
export TEST_DATABASE_URL=postgres://api_scan:api_scan_secret@localhost:5432/api_scan
export TEST_BILLING_DATABASE_URL=postgres://billing:billing_secret@localhost:5433/billing_db
go test ./services/...
```

### 5. Updating Documentation

After changing API handlers, regenerate Swagger:

```bash
# For Billing Service
cd services/billing
~/go/bin/swag init -g cmd/server/main.go

# For API Gateway
cd services/api-gateway
~/go/bin/swag init -g cmd/server/main.go

# For Cabinet
cd services/cabinet
~/go/bin/swag init -g cmd/server/main.go
```

### 6. Commit Messages

Follow conventional commits:
```
feat: add new feature
fix: fix bug
docs: update documentation
test: add tests
refactor: code refactoring
chore: maintenance tasks
```

### 7. Pull Request Process

1. Create feature branch: `git checkout -b feature/name`
2. Make changes and add tests
3. Update documentation
4. Run tests: `go test ./...`
5. Commit and push
6. Create PR with description
