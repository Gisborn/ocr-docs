# API-Scan: OCR Service for Russian Passports

> Cloud API service for recognizing Russian Federation passports, designed for integration with 1C and other B2B systems.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Technology Stack](#technology-stack)
3. [Services](#services)
4. [API Documentation](#api-documentation)
5. [Business Processes](#business-processes)
6. [Testing](#testing)
7. [Getting Started](#getting-started)
8. [Security](#security)
9. [Monitoring](#monitoring)

---

## Architecture Overview

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLIENTS                                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐    │
│  │   1C ERP    │  │   Web App   │  │ Mobile App  │  │  Cabinet (Web)  │    │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └────────┬────────┘    │
└─────────┼────────────────┼────────────────┼─────────────────┼─────────────┘
          │                │                │                 │
          └────────────────┴────────────────┘                 │
                           │                                  │
                           ▼                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           API GATEWAY (Port 8080)                           │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │  • Authentication (API Keys, bcrypt)                                   │ │
│  │  • Rate Limiting (Redis, 10 RPS default)                               │ │
│  │  • Routing to downstream services                                      │ │
│  │  • X-Request-ID generation                                             │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
          │
          ├────────────────────┬────────────────────┬────────────────────┐
          │                    │                    │                    │
          ▼                    ▼                    ▼                    ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│  ORCHESTRATOR   │  │    BILLING      │  │    BILLING      │  │    CABINET      │
│   (Port 8083)   │  │   (Port 8081)   │  │  WEBHOOK-YOOK   │  │   (Port 8084)   │
│                 │  │                 │  │   (Port 8082)   │  │                 │
│ • OCR Processing│  │ • Accounts      │  │                 │  │ • Registration  │
│ • Yandex Vision │  │ • Reserve/Commit│  │ • YooKassa      │  │ • Auth (sessions│
│ • VK Fallback   │  │ • Subscriptions │  │   Webhooks      │  │ • API Keys mgmt │
│ • Normalizer    │  │ • Payments      │  │ • IP Whitelist  │  │ • Account events│
└────────┬────────┘  └────────┬────────┘  └─────────────────┘  └─────────────────┘
         │                    │
         │           ┌────────┴────────┐
         │           │                 │
         ▼           ▼                 ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│   PostgreSQL    │  │   PostgreSQL    │  │     Redis       │
│  api_scan_main  │  │   billing_db    │  │                 │
│  (Port 5432)    │  │   (Port 5433)   │  │   (Port 6379)   │
│                 │  │                 │  │                 │
│ • organizations │  │ • accounts      │  │ • Rate limiting │
│ • users         │  │ • billing_events│  │ • Sessions      │
│ • api_keys      │  │ • subscriptions │  │ • API Key cache │
│ • account_events│  │ • payment_orders│  │                 │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

### Service Ports

| Service | Port | Database | Description |
|---------|------|----------|-------------|
| API Gateway | 8080 | Redis | Entry point, auth, rate limiting |
| Billing | 8081 | billing_db | Payments, subscriptions, transactions |
| Billing Webhook | 8082 | billing_db | YooKassa webhook handler |
| Orchestrator | 8083 | - | OCR processing, Yandex/VK Vision |
| Cabinet | 8084 | api_scan_main | Personal account, API keys |
| PostgreSQL (main) | 5432 | api_scan | Organizations, users, keys |
| PostgreSQL (billing) | 5433 | billing_db | Accounts, transactions |
| Redis | 6379 | - | Cache, rate limiting |

### Data Flow: Passport Recognition

```
1. Client → API Gateway (POST /v1/recognize)
   Headers: X-Api-Key, Idempotency-Key

2. API Gateway → Validates API Key (bcrypt + Redis cache)
   → Checks Rate Limit (Redis sliding window)
   → Routes to Orchestrator

3. Orchestrator → POST /accounts/{id}/reserve (Billing)
   → Gets billing account from context

4. Billing → Checks balance (snapshot + events - reservations)
   → Creates reservation (5 min TTL)
   → Returns reservation ID

5. Orchestrator → Calls Yandex Vision OCR
   → If fails/timeout → Fallback to VK Vision
   → Circuit Breaker tracks failures

6. Orchestrator → Normalizes OCR result
   → Extracts: ФИО, дата рождения, серия/номер, etc.
   → Confidence scoring per field

7. Orchestrator → POST /transactions/{id}/commit (Billing)
   → On success: commits reservation, creates billing_event
   → On error: POST /transactions/{id}/rollback

8. Orchestrator → Returns JSON to API Gateway → Client
   Response: {request_id, fields, confidences, provider}
```

---

## Technology Stack

### Backend
- **Language**: Go 1.25
- **HTTP Router**: Standard library `net/http`
- **Database**: PostgreSQL 16 (pgx/v5 driver)
- **Cache**: Redis 7 (go-redis/v9)
- **Migrations**: goose

### OCR Providers
- **Primary**: Yandex Vision API
- **Fallback**: VK Vision API
- **Circuit Breaker**: Custom implementation

### Authentication & Security
- **Password Hashing**: bcrypt
- **API Keys**: base64(id:secret), bcrypt hashed
- **Sessions**: HTTP-only cookies
- **Rate Limiting**: Redis sliding window

### Payments
- **Provider**: YooKassa (ЮКасса)
- **Webhooks**: HMAC-SHA256 verification, IP whitelist

### Documentation
- **OpenAPI**: swaggo/swag
- **Swagger UI**: http-swagger

### Infrastructure
- **Containerization**: Docker, Docker Compose
- **Databases**: PostgreSQL (2 instances)
- **Cache**: Redis

---

## Services

### 1. API Gateway (Port 8080)

**Purpose**: Single entry point for all API requests

**Features**:
- API Key authentication (bcrypt verification)
- Rate limiting (10 RPS default, Redis-based)
- Request routing to downstream services
- X-Request-ID propagation
- CORS headers

**Environment Variables**:
```bash
PORT=8080
DATABASE_URL=postgres://api_scan:api_scan_secret@postgres:5432/api_scan
REDIS_ADDR=redis:6379
ORCHESTRATOR_URL=http://orchestrator:8080
BILLING_URL=http://billing:8080
BILLING_WEBHOOK_URL=http://billing-webhook-yookassa:8080
```

**Routes**:
| Path | Target Service | Auth Required |
|------|----------------|---------------|
| `/health` | - | No |
| `/swagger/` | - | No |
| `/v1/recognize` | Orchestrator | Yes |
| `/v1/billing/*` | Billing | Yes |
| `/webhooks/*` | Billing Webhook | No (IP whitelist) |

---

### 2. Billing Service (Port 8081)

**Purpose**: Financial operations, subscriptions, payments

**Architecture Pattern**: Event Sourcing
- `billing_events` - immutable transaction log
- `balance_snapshots` - read-optimized cache
- `reservations` - pending transactions (TTL 5 min)

**Endpoints**:
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/accounts` | POST | Create account |
| `/accounts/{id}/balance` | GET | Get balance |
| `/accounts/{id}/reserve` | POST | Reserve funds |
| `/transactions/{id}/commit` | POST | Commit transaction |
| `/transactions/{id}/rollback` | POST | Rollback transaction |
| `/accounts/{id}/subscriptions` | POST | Create subscription |
| `/accounts/{id}/subscriptions/upgrade` | POST | Upgrade subscription |
| `/accounts/{id}/payments` | POST | Create payment |
| `/payments/{id}` | GET | Get payment status |

**Two-Phase Commit Flow**:
```
1. RESERVE: Check balance → Create reservation (PENDING)
2a. COMMIT: Create billing_event → Delete reservation (COMMITTED)
2b. ROLLBACK: Delete reservation (ROLLED_BACK)
```

**Balance Calculation**:
```
real_balance = snapshot.real_balance_rub 
               + SUM(events.real_amount_rub since snapshot)
               - SUM(reservations.real_amount_rub)
```

---

### 3. Billing Webhook YooKassa (Port 8082)

**Purpose**: Handle YooKassa payment webhooks

**Security**:
- IP Whitelist: `185.71.76.0/27`, `185.71.77.0/27`, `77.75.153.0/25`
- HMAC-SHA256 signature verification (optional)
- Idempotency by `payment_id`

**Endpoints**:
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/webhooks/yookassa` | POST | Payment notification |

**Webhook Events**:
- `payment.succeeded` → Create billing_event (balance_topup), mark order completed
- `payment.canceled` → Mark order failed

**Idempotency**: If order already `completed`, return 200 without duplicate processing.

---

### 4. Orchestrator (Port 8083)

**Purpose**: OCR processing with fallback

**Features**:
- Yandex Vision (primary) / VK Vision (fallback)
- Circuit Breaker (5 errors in 60s → 30s cooldown)
- Passport normalizer (confidence scoring)
- Billing integration (Reserve/Commit/Rollback)

**Endpoints**:
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/stats` | GET | Circuit breaker stats |
| `/v1/recognize` | POST | Recognize passport |

**Recognize Request**:
```bash
POST /v1/recognize
Headers:
  Content-Type: multipart/form-data
  X-Api-Key: {api_key}
  Idempotency-Key: {unique_id}

Body:
  file: {image_file}
```

**Recognize Response**:
```json
{
  "request_id": "req_abc123",
  "document_type": "passport_rf",
  "fields": {
    "last_name": "Иванов",
    "first_name": "Иван",
    "middle_name": "Иванович",
    "birth_date": "01.01.1990",
    "series": "4515",
    "number": "123456",
    "issue_date": "15.05.2015",
    "issued_by": "Отделом УФМС...",
    "division_code": "770-064",
    "registration_address": null
  },
  "confidences": {
    "last_name": 0.95,
    "first_name": 0.95,
    "series": 0.90,
    "number": 0.92
  },
  "provider": "yandex"
}
```

---

### 5. Cabinet Service (Port 8084)

**Purpose**: Personal account management

**Features**:
- Organization registration
- User authentication (sessions)
- API key management (max 10 keys)
- Account event history

**Endpoints**:
| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/health` | GET | No | Health check |
| `/swagger/` | GET | No | Swagger UI |
| `/api/v1/auth/register` | POST | No | Register organization |
| `/api/v1/auth/login` | POST | No | Login |
| `/api/v1/auth/logout` | POST | Yes | Logout |
| `/api/v1/api-keys` | GET | Yes | List API keys |
| `/api/v1/api-keys` | POST | Yes | Create API key |
| `/api/v1/api-keys/{id}` | DELETE | Yes | Revoke API key |

**Registration Flow**:
```
1. POST /api/v1/auth/register
   → Create organization (status: pending)
   → Create user (role: admin)
   → Send verification email (TODO)

2. (Future) Email verification
   → Set email_verified = true
   → Set status = active
   → POST /accounts (Billing Service)
   → Save billing_account_id
```

**API Key Format**:
```
base64(key_id:secret)
Example: MTIzOnNrX2xpdmVfYWJjMTIzeHl6

Storage: bcrypt(full_key)
Display: ****XXXX (last 4 chars only, once on creation)
```

---

## API Documentation

### Swagger UI URLs

| Service | URL |
|---------|-----|
| API Gateway | http://localhost:8080/swagger/ |
| Billing | http://localhost:8081/swagger/ |
| Cabinet | http://localhost:8084/swagger/ |

### Quick Start Examples

#### 1. Register Organization
```bash
curl -X POST http://localhost:8084/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "organization_name": "My Company",
    "email": "admin@example.com",
    "password": "secure_password"
  }'
```

#### 2. Login
```bash
curl -X POST http://localhost:8084/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@example.com",
    "password": "secure_password"
  }'
# Returns: {"session_token": "...", "user": {...}}
```

#### 3. Create API Key
```bash
curl -X POST http://localhost:8084/api/v1/api-keys \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer {session_token}" \
  -d '{"name": "Production Key"}'
# Returns: {"full_key": "cabinet_...", ...}
# IMPORTANT: Save full_key, it won't be shown again!
```

#### 4. Recognize Passport
```bash
curl -X POST http://localhost:8080/v1/recognize \
  -H "X-Api-Key: {full_key}" \
  -H "Idempotency-Key: unique-request-id-123" \
  -F "file=@passport.jpg"
```

---

## Business Processes

### Process 1: New Customer Onboarding

```mermaid
sequenceDiagram
    actor User
    participant Cabinet
    participant Billing
    participant DB

    User->>Cabinet: POST /api/v1/auth/register
    Cabinet->>DB: Create organization (pending)
    Cabinet->>DB: Create user (admin)
    Cabinet-->>User: Registration success
    
    Note over User: Email verification (TODO)
    
    User->>Cabinet: POST /api/v1/auth/login
    Cabinet->>DB: Verify credentials
    Cabinet->>DB: Create session
    Cabinet-->>User: Session token
    
    User->>Cabinet: POST /api/v1/api-keys
    Cabinet->>DB: Check limit (max 10)
    Cabinet->>DB: Create API key (bcrypt hash)
    Cabinet-->>User: Full key (shown once)
```

### Process 2: Passport Recognition with Payment

```mermaid
sequenceDiagram
    actor Client
    participant Gateway
    participant Orchestrator
    participant Billing
    participant YandexVision
    participant Normalizer

    Client->>Gateway: POST /v1/recognize
    Gateway->>Gateway: Validate API Key
    Gateway->>Gateway: Check rate limit
    Gateway->>Orchestrator: Proxy request
    
    Orchestrator->>Billing: POST /accounts/{id}/reserve
    Billing->>Billing: Check balance
    Billing->>Billing: Create reservation
    Billing-->>Orchestrator: Reserved
    
    Orchestrator->>YandexVision: OCR request
    YandexVision-->>Orchestrator: Raw text
    
    Orchestrator->>Normalizer: Normalize text
    Normalizer-->>Orchestrator: Structured data
    
    alt Success
        Orchestrator->>Billing: POST /transactions/{id}/commit
        Billing->>Billing: Create billing_event
        Billing->>Billing: Delete reservation
    else Error
        Orchestrator->>Billing: POST /transactions/{id}/rollback
        Billing->>Billing: Delete reservation
    end
    
    Orchestrator-->>Gateway: Result
    Gateway-->>Client: JSON response
```

### Process 3: Balance Top-up via YooKassa

```mermaid
sequenceDiagram
    actor User
    participant Cabinet
    participant Billing
    participant YooKassa
    participant Webhook

    User->>Cabinet: Click "Top up balance"
    Cabinet->>Billing: POST /accounts/{id}/payments
    Billing->>Billing: Create payment_order (pending)
    Billing->>YooKassa: Create payment
    YooKassa-->>Billing: Payment URL
    Billing-->>Cabinet: Redirect URL
    Cabinet-->>User: Redirect to YooKassa
    
    User->>YooKassa: Complete payment
    YooKassa->>Webhook: POST /webhooks/yookassa
    Webhook->>Webhook: Verify IP & signature
    Webhook->>Webhook: Check idempotency
    Webhook->>Billing: Update payment_order (completed)
    Webhook->>Billing: Create billing_event (balance_topup)
    Webhook-->>YooKassa: 200 OK
    
    YooKassa-->>User: Redirect to success page
```

---

## Testing

### Unit Tests

**Run all tests**:
```bash
cd /home/alexey/repo/ocr-passport
go test ./...
```

**Run tests for specific service**:
```bash
# Billing Service
go test ./services/billing/internal/service/... -v

# API Gateway Middleware
go test ./services/api-gateway/internal/middleware/... -v

# Orchestrator Handler
go test ./services/orchestrator/internal/handler/... -v

# Billing Webhook
go test ./services/billing-webhook-yookassa/internal/handler/... -v
```

### Test Coverage

| Service | Tests | Coverage |
|---------|-------|----------|
| Billing | 15 | Reserve/Commit/Rollback, Subscriptions, Payments |
| API Gateway | 15 | Auth, Rate Limiting |
| Billing Webhook | 9 | Webhook processing, Idempotency |
| Orchestrator | 9 | Handler, Image reading |
| Normalizer | 4 | FIO, Series/Number, Division code |

### Integration Testing

**Manual test flow**:
```bash
# 1. Start infrastructure
docker-compose up -d postgres postgres-billing redis

# 2. Run migrations
cd migrations/main && goose up
cd migrations/billing && goose up

# 3. Start services
docker-compose --profile billing --profile gateway --profile cabinet up -d

# 4. Run integration tests (TODO)
```

---

## Getting Started

### Prerequisites
- Docker 24+
- Docker Compose 2+
- Go 1.25 (for local development)
- Make (optional)

### Quick Start with Docker Compose

```bash
# Clone repository
git clone <repo-url>
cd api-scan

# Start all services
docker-compose --profile billing --profile gateway --profile cabinet up -d

# Check status
docker-compose ps

# View logs
docker-compose logs -f api-gateway

# Stop everything
docker-compose down
```

### Access Points

| Service | URL |
|---------|-----|
| API Gateway | http://localhost:8080 |
| Billing API | http://localhost:8081 |
| Billing Webhook | http://localhost:8082 |
| Cabinet | http://localhost:8084 |
| Swagger (Gateway) | http://localhost:8080/swagger/ |
| Swagger (Billing) | http://localhost:8081/swagger/ |
| Swagger (Cabinet) | http://localhost:8084/swagger/ |

### Environment Variables

**Required for all services**:
```bash
# Database
DATABASE_URL=postgres://user:pass@host:5432/dbname

# For API Gateway
REDIS_ADDR=redis:6379
ORCHESTRATOR_URL=http://orchestrator:8080
BILLING_URL=http://billing:8080

# For Billing
YOOKASSA_SHOP_ID=your_shop_id
YOOKASSA_SECRET_KEY=your_secret_key

# For Cabinet
BILLING_URL=http://billing:8080
BILLING_SERVICE_TOKEN=service_token
```

---

## Security

### Authentication

**API Key Verification**:
1. Parse `X-Api-Key` header (base64)
2. Extract `key_id:secret`
3. Fetch key hash from DB (or Redis cache)
4. Compare using `bcrypt.CompareHashAndPassword`
5. Check organization status (active)
6. Update `last_used_at` (async)

**Session Authentication (Cabinet)**:
1. Login → Create session token
2. Set HTTP-only cookie `session={token}`
3. Subsequent requests: validate token
4. Logout → Delete session

### Rate Limiting

**Algorithm**: Sliding Window (Redis Sorted Sets)
- Key: `rate_limit:{api_key_id}`
- Window: 1 second
- Default limit: 10 requests/second
- Headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`

### Data Protection

- **Passwords**: bcrypt with cost 10
- **API Keys**: bcrypt hash stored, never in logs
- **PII**: Passport data not stored (stateless processing)
- **TLS**: Required for all connections (1.2+)
- **Database**: Encrypted at rest (Yandex Cloud)

### Network Security

- **IP Whitelist**: Webhook service only accepts YooKassa IPs
- **Internal Network**: Services communicate via Docker network
- **No Direct DB Access**: Only via services

---

## Monitoring

### Health Checks

All services expose:
```bash
GET /health

Response:
{
  "status": "ok"
}
```

### Metrics (TODO)

| Metric | Service | Description |
|--------|---------|-------------|
| `http_requests_total` | All | Request count by status |
| `http_request_duration_seconds` | All | Latency histogram |
| `ocr_confidence_score` | Orchestrator | Recognition confidence |
| `circuit_breaker_state` | Orchestrator | Open/Closed/Half-open |
| `billing_balance_rub` | Billing | Account balances |
| `pending_reservations_count` | Billing | Stuck reservations |

### Logging

- Format: Structured JSON
- Levels: INFO, ERROR
- Request ID propagation via `X-Request-ID`

---

## Project Structure

```
.
├── docker-compose.yml          # Infrastructure orchestration
├── go.mod                      # Go dependencies
├── Makefile                    # Build commands
├── README.md                   # This file
├── docs/                       # Documentation
├── migrations/
│   ├── main/                   # api_scan_main migrations
│   └── billing/                # billing_db migrations
├── pkg/
│   ├── billing/                # Shared billing types
│   ├── normalizer/             # Passport data normalizer
│   └── ocr/                    # OCR provider interfaces
└── services/
    ├── api-gateway/            # Entry point service
    ├── billing/                # Billing service
    ├── billing-webhook-yookassa/  # YooKassa webhook handler
    ├── cabinet/                # Personal account service
    └── orchestrator/           # OCR processing service
```

---

## License

Apache 2.0

## Support

- Email: support@api-scan.example.com
- Issues: GitHub Issues
