#!/usr/bin/env bash
# Ручной деплой на production-сервер (Ubuntu)
# Требования: Docker, Docker Compose plugin, доступ к GHCR
#
# Использование:
#   ./scripts/deploy.sh
#
# Перед первым запуском:
#   1. Скопируйте .env на сервер в ~/api-scan/
#   2. Выполните docker login ghcr.io (с PAT от GitHub)

set -euo pipefail

APP_DIR="${APP_DIR:-$HOME/api-scan}"
COMPOSE_FILE="${APP_DIR}/infra/docker/docker-compose.prod.yml"
ENV_FILE="${APP_DIR}/.env"

echo "=== API-Scan Production Deploy ==="
echo "App dir: ${APP_DIR}"
echo "Compose file: ${COMPOSE_FILE}"
echo ""

# Проверяем наличие .env
if [[ ! -f "${ENV_FILE}" ]]; then
    echo "ERROR: .env file not found at ${ENV_FILE}"
    echo "Create it first: cp .env.example .env && nano .env"
    exit 1
fi

cd "${APP_DIR}"

echo "=== Pulling latest images ==="
docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" pull

echo "=== Running database migrations ==="
source "${ENV_FILE}"
cd ~/api-scan/migrations/main && goose postgres "postgres://${POSTGRES_USER:-api_scan}:${POSTGRES_PASSWORD}@localhost:5432/${POSTGRES_DB:-api_scan}?sslmode=disable" up
cd ~/api-scan/migrations/billing && goose postgres "postgres://${POSTGRES_BILLING_USER:-billing}:${POSTGRES_BILLING_PASSWORD}@localhost:5432/${POSTGRES_BILLING_DB:-billing_db}?sslmode=disable" up
cd ~/api-scan

echo "=== Starting services ==="
docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" up -d --remove-orphans

echo "=== Cleanup ==="
docker system prune -f

echo ""
echo "=== Deployment complete ==="
echo "Check status: docker compose -f ${COMPOSE_FILE} ps"
echo "Logs: docker compose -f ${COMPOSE_FILE} logs -f"
