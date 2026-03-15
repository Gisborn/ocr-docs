#!/bin/bash
# Скрипт для проверки подключения к PostgreSQL

set -e

echo "=== Проверка подключения к PostgreSQL ==="

# Параметры подключения
DB_HOST=${DB_HOST:-localhost}
DB_PORT=${DB_PORT:-5432}
DB_USER=${DB_USER:-api_scan}
DB_NAME=${DB_NAME:-api_scan}
DB_PASSWORD=${DB_PASSWORD:-api_scan_secret}

export PGPASSWORD="$DB_PASSWORD"

echo "Хост: $DB_HOST:$DB_PORT"
echo "База: $DB_NAME"
echo "Пользователь: $DB_USER"
echo ""

# Проверка доступности
echo "1. Проверка доступности PostgreSQL..."
if pg_isready -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME"; then
    echo "   ✓ PostgreSQL доступен"
else
    echo "   ✗ PostgreSQL НЕ доступен"
    exit 1
fi

echo ""
echo "2. Проверка списка таблиц..."
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "\dt" | grep -E "(organizations|users|api_keys|tariffs|billing_transactions)" || true

echo ""
echo "3. Проверка тестовых данных..."
echo "   Организации:"
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "SELECT id, name, email, balance_rub FROM organizations;"

echo ""
echo "   API-ключи:"
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "SELECT id, org_id, name, created_at FROM api_keys;"

echo ""
echo "   Тарифы:"
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "SELECT id, code, name, type FROM tariffs;"

echo ""
echo "=== Проверка завершена ==="
