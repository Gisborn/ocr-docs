#!/bin/bash
# Quick end-to-end test script for local development

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=========================================="
echo "  API-Scan Quick E2E Test"
echo "=========================================="

# Check if services are running
echo -e "\n${YELLOW}[1/7] Checking services...${NC}"
for port in 8080 8081 8082 8083 8084; do
    if curl -s "http://localhost:$port/health" > /dev/null 2>&1; then
        echo "  ✓ Port $port: UP"
    else
        echo "  ✗ Port $port: DOWN"
        echo "  Run: make docker-up"
        exit 1
    fi
done

# Test 1: Cabinet Login
echo -e "\n${YELLOW}[2/7] Testing Cabinet Login...${NC}"
LOGIN_RESPONSE=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
    -H "Content-Type: application/json" \
    -d '{"email":"test@example.com","password":"password"}')

if echo "$LOGIN_RESPONSE" | grep -q "session_token"; then
    SESSION_TOKEN=$(echo "$LOGIN_RESPONSE" | grep -o '"session_token":"[^"]*"' | cut -d'"' -f4)
    echo "  ✓ Login successful"
else
    echo "  ✗ Login failed"
    echo "  Response: $LOGIN_RESPONSE"
    exit 1
fi

# Test 2: Create API Key
echo -e "\n${YELLOW}[3/7] Testing API Key creation...${NC}"
KEY_RESPONSE=$(curl -s -X POST http://localhost:8084/api/v1/api-keys \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $SESSION_TOKEN" \
    -d '{"name":"Test Key"}')

if echo "$KEY_RESPONSE" | grep -q "full_key"; then
    FULL_KEY=$(echo "$KEY_RESPONSE" | grep -o '"full_key":"[^"]*"' | cut -d'"' -f4)
    echo "  ✓ API Key created"
    echo "  Key: ${FULL_KEY:0:20}..."
else
    echo "  ✗ API Key creation failed"
    echo "  Response: $KEY_RESPONSE"
    exit 1
fi

# Test 3: Check Balance via API Gateway
echo -e "\n${YELLOW}[4/7] Testing Balance check...${NC}"
BALANCE_RESPONSE=$(curl -s -X GET "http://localhost:8080/v1/billing/accounts/1/balance" \
    -H "X-Api-Key: $FULL_KEY")

if echo "$BALANCE_RESPONSE" | grep -q "account_id"; then
    PREPAID=$(echo "$BALANCE_RESPONSE" | grep -o '"prepaid_balance_rub":[0-9]*' | cut -d':' -f2)
    echo "  ✓ Balance retrieved"
    echo "  Prepaid balance: ${PREPAID:-0}₽"
else
    echo "  ✗ Balance check failed"
    echo "  Response: $BALANCE_RESPONSE"
    exit 1
fi

# Test 4: Reserve funds
echo -e "\n${YELLOW}[5/7] Testing Reserve funds...${NC}"
RESERVE_RESPONSE=$(curl -s -X POST "http://localhost:8080/v1/billing/accounts/1/reserve" \
    -H "Content-Type: application/json" \
    -H "X-Api-Key: $FULL_KEY" \
    -H "Idempotency-Key: test-$(date +%s)" \
    -d '{"amount_rub":100}')

if echo "$RESERVE_RESPONSE" | grep -q "request_id"; then
    REQUEST_ID=$(echo "$RESERVE_RESPONSE" | grep -o '"request_id":"[^"]*"' | cut -d'"' -f4)
    echo "  ✓ Reserved 100₽"
    echo "  Request ID: $REQUEST_ID"
else
    echo "  ✗ Reserve failed"
    echo "  Response: $RESERVE_RESPONSE"
    exit 1
fi

# Test 5: Commit transaction
echo -e "\n${YELLOW}[6/7] Testing Commit transaction...${NC}"
COMMIT_RESPONSE=$(curl -s -X POST "http://localhost:8080/v1/billing/transactions/${REQUEST_ID}/commit" \
    -H "X-Api-Key: $FULL_KEY")

if [ "$COMMIT_RESPONSE" = "{}" ] || echo "$COMMIT_RESPONSE" | grep -q "success"; then
    echo "  ✓ Transaction committed"
else
    echo "  ✗ Commit failed"
    echo "  Response: $COMMIT_RESPONSE"
    exit 1
fi

# Test 6: Check updated balance
echo -e "\n${YELLOW}[7/7] Checking updated balance...${NC}"
NEW_BALANCE=$(curl -s -X GET "http://localhost:8080/v1/billing/accounts/1/balance" \
    -H "X-Api-Key: $FULL_KEY")

NEW_PREPAID=$(echo "$NEW_BALANCE" | grep -o '"prepaid_balance_rub":[0-9]*' | cut -d':' -f2)
echo "  ✓ New prepaid balance: ${NEW_PREPAID:-0}₽"

# Summary
echo ""
echo "=========================================="
echo -e "${GREEN}  ✓ All tests passed!${NC}"
echo "=========================================="
echo ""
echo "Next steps:"
echo "  1. Open Cabinet: http://localhost:8084/swagger/"
echo "  2. Open API Gateway: http://localhost:8080/swagger/"
echo "  3. Test with real image:"
echo "     curl -X POST http://localhost:8080/v1/recognize \\"
echo "       -H 'X-Api-Key: $FULL_KEY' \\"
echo "       -F 'file=@your_passport.jpg'"
