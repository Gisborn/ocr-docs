#!/bin/bash
# E2E тесты как в CI (с образами GHCR :latest)
# Запускает полный стек из docker-compose.test.yml с pull из registry
#
# Использование:
#   ./scripts/e2e-test-ci.sh
#
# Переменные окружения:
#   DB_WAIT      — секунд ожидания перед миграциями (default: 20)
#   TEST_TIMEOUT — таймаут go test (default: 120s)

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

COMPOSE_FILE="infra/docker/docker-compose.test.yml"
DB_WAIT=${DB_WAIT:-20}
TEST_TIMEOUT=${TEST_TIMEOUT:-120s}

cleanup() {
    echo ""
    echo -e "${YELLOW}Очистка окружения...${NC}"
    docker compose -f "$COMPOSE_FILE" down -v || true
}
trap cleanup EXIT

echo "=========================================="
echo "  E2E Tests (CI mode — GHCR images)"
echo "=========================================="

# Проверка инструментов
command -v docker >/dev/null 2>&1 || { echo -e "${RED}docker required${NC}"; exit 1; }
command -v go >/dev/null 2>&1 || { echo -e "${RED}go required${NC}"; exit 1; }

# 1. Поднять окружение
echo -e "\n${YELLOW}[1/4] Starting test environment...${NC}"
docker compose -f "$COMPOSE_FILE" pull
docker compose -f "$COMPOSE_FILE" up -d --pull always --force-recreate

echo -e "${YELLOW}Waiting ${DB_WAIT}s for services...${NC}"
sleep "$DB_WAIT"

# 2. Миграции
echo -e "\n${YELLOW}[2/4] Running migrations...${NC}"
go install github.com/pressly/goose/v3/cmd/goose@v3.24.0 2>/dev/null || true

export DATABASE_URL="postgres://api_scan:api_scan_secret@localhost:15432/api_scan?sslmode=disable"
export BILLING_DATABASE_URL="postgres://billing:billing_secret@localhost:15433/billing_db?sslmode=disable"

( cd migrations/main && goose postgres "$DATABASE_URL" up )
( cd migrations/billing && goose postgres "$BILLING_DATABASE_URL" up )

# 3. E2E тесты
echo -e "\n${YELLOW}[3/4] Running E2E tests...${NC}"
export CABINET_URL=http://localhost:8084
export API_GATEWAY_URL=http://localhost:8080
export GOTOOLCHAIN=go1.24.0

if go test -v -timeout="$TEST_TIMEOUT" ./tests/integration/...; then
    echo -e "\n${GREEN}=========================================="
    echo "  ✓ All E2E tests passed!"
    echo "==========================================${NC}"
else
    echo -e "\n${RED}=========================================="
    echo "  ✗ E2E tests failed!"
    echo "==========================================${NC}"
    echo -e "\n${YELLOW}Dumping service logs...${NC}"
    docker compose -f "$COMPOSE_FILE" logs --tail=100
    exit 1
fi
