#!/bin/bash
# E2E тесты с локально собранными образами (текущий код)
# Собирает сервисы из исходников и запускает полный стек
#
# Использование:
#   ./scripts/e2e-test-local.sh
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
echo "  E2E Tests (Local build)"
echo "=========================================="

# Проверка инструментов
command -v docker >/dev/null 2>&1 || { echo -e "${RED}docker required${NC}"; exit 1; }
command -v go >/dev/null 2>&1 || { echo -e "${RED}go required${NC}"; exit 1; }

# 1. Сборка локальных образов
echo -e "\n${YELLOW}[1/5] Building local images...${NC}"

docker build -t ghcr.io/gisborn/ocr-docs/api-gateway:latest -f services/api-gateway/Dockerfile .
docker build -t ghcr.io/gisborn/ocr-docs/billing:latest -f services/billing/Dockerfile .
docker build -t ghcr.io/gisborn/ocr-docs/cabinet:latest -f services/cabinet/Dockerfile .
docker build -t ghcr.io/gisborn/ocr-docs/orchestrator:latest -f services/orchestrator/Dockerfile .

echo -e "${GREEN}✓ Local images built${NC}"

# 2. Поднять окружение (без --pull always, используем локальные образы)
echo -e "\n${YELLOW}[2/5] Starting test environment...${NC}"
docker compose -f "$COMPOSE_FILE" up -d --force-recreate

echo -e "${YELLOW}Waiting ${DB_WAIT}s for services...${NC}"
sleep "$DB_WAIT"

# 3. Миграции
echo -e "\n${YELLOW}[3/5] Running migrations...${NC}"
go install github.com/pressly/goose/v3/cmd/goose@v3.24.0 2>/dev/null || true

export DATABASE_URL="postgres://api_scan:api_scan_secret@localhost:15432/api_scan?sslmode=disable"
export BILLING_DATABASE_URL="postgres://billing:billing_secret@localhost:15433/billing_db?sslmode=disable"

( cd migrations/main && goose postgres "$DATABASE_URL" up )
( cd migrations/billing && goose postgres "$BILLING_DATABASE_URL" up )

# 4. E2E тесты
echo -e "\n${YELLOW}[4/5] Running E2E tests...${NC}"
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
