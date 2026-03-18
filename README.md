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
│  │  • CORS support for browser clients                                    │ │
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
│ • Normalizer    │  │ • Payments      │  │ • IP Whitelist  │  │ • Web UI        │
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
│ • sessions      │  │ • reservations  │  │                 │
│ • account_events│  │ • balance_snap  │  │                 │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

### Service Ports

| Service | Port | Database | Description |
|---------|------|----------|-------------|
| API Gateway | 8080 | Redis | Entry point, auth, rate limiting, CORS |
| Billing | 8081 | billing_db | Payments, subscriptions, transactions |
| Billing Webhook | 8082 | billing_db | YooKassa webhook handler |
| Orchestrator | 8083 | - | OCR processing, Yandex/VK Vision |
| Cabinet | 8084 | api_scan_main | Personal account, API keys, Web UI |
| PostgreSQL (main) | 5432 | api_scan | Organizations, users, keys, sessions |
| PostgreSQL (billing) | 5433 | billing_db | Accounts, transactions, billing |
| Redis | 6379 | - | Cache, rate limiting, sessions |

---

## Technology Stack

- **Go 1.25** - основной язык
- **PostgreSQL 16** - базы данных
- **Redis 7** - кэш и rate limiting
- **Docker & Docker Compose** - контейнеризация
- **Swagger/OpenAPI** - документация API

---

## Services

### 1. API Gateway (Port 8080)

**Purpose**: Entry point for all API requests

**Features**:
- API Key authentication (bcrypt)
- Rate limiting (Redis, 10 RPS default)
- CORS support for browser clients
- Request logging
- Routing to downstream services

**Endpoints**:
| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/health` | GET | No | Health check |
| `/v1/recognize` | POST | API Key | Recognize passport |
| `/v1/billing/*` | ALL | API Key | Billing operations |
| `/webhooks/yookassa` | POST | No | YooKassa webhooks |

### 2. Billing Service (Port 8081)

**Purpose**: Payment processing, subscriptions, balance management

**Features**:
- Two-phase commit (Reserve/Commit/Rollback)
- Balance calculation (snapshot + events)
- Subscription management
- YooKassa integration

**Key Concepts**:
```
Balance = Snapshot Balance + Events Since Snapshot - Active Reservations
```

**Endpoints**:
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/accounts` | POST | Create account |
| `/accounts/{id}/balance` | GET | Get balance |
| `/accounts/{id}/reserve` | POST | Reserve funds |
| `/transactions/{id}/commit` | POST | Commit transaction |
| `/transactions/{id}/rollback` | POST | Rollback transaction |

### 3. Cabinet Service (Port 8084)

**Purpose**: Personal account management with Web UI

**Features**:
- Organization registration
- User authentication (sessions)
- API key management (max 10 keys)
- Web UI at `/`
- Swagger docs at `/swagger/`

**Test Credentials**:
- Email: `test@example.com`
- Password: `password`

**Endpoints**:
| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/` | GET | No | Web UI (Личный Кабинет) |
| `/api/v1/auth/register` | POST | No | Register |
| `/api/v1/auth/login` | POST | No | Login |
| `/api/v1/auth/logout` | POST | Session | Logout |
| `/api/v1/api-keys` | GET | Session | List keys |
| `/api/v1/api-keys` | POST | Session | Create key |

---

## API Documentation

Swagger UI доступен для каждого сервиса:
- **API Gateway**: http://localhost:8080/swagger/
- **Billing**: http://localhost:8081/swagger/
- **Cabinet**: http://localhost:8084/swagger/

---

## Business Processes

### API Key Authentication Flow

```
1. Client requests API key from Cabinet
   POST /api/v1/api-keys (Session Auth)
   
2. Cabinet generates key: base64(key_id:secret)
   Stores bcrypt hash in DB
   
3. Client uses key in requests:
   X-Api-Key: base64(key_id:secret)
   
4. API Gateway validates:
   - Parses base64 → key_id:secret
   - Gets key hash from DB
   - Compares bcrypt(full_key, stored_hash)
   - Checks rate limit
```

### Two-Phase Commit Flow

```
1. RESERVE: Check balance → Create reservation (PENDING)
2a. COMMIT: Create billing_event → Delete reservation (COMMITTED)
2b. ROLLBACK: Delete reservation (ROLLED_BACK)
```

---

## Testing

### Unit Tests (with mocks)

```bash
# Run all unit tests
make test

# Run specific service tests
go test ./services/billing/...
go test ./services/api-gateway/...
```

### Integration Tests (with real DB)

```bash
# 1. Start all services
make docker-up

# 2. Run integration tests
make test-integration
```

Integration tests cover:
- Registration and login flow
- Session management
- API key creation and validation
- Balance retrieval

### Quick Test Script

```bash
# Test login
TOKEN=$(curl -s -X POST http://localhost:8084/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password"}' | jq -r '.session_token')

# Create API key
KEY=$(curl -s -X POST http://localhost:8084/api/v1/api-keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Key"}' | jq -r '.full_key')

# Check balance via API Gateway
curl http://localhost:8080/v1/billing/accounts/1/balance \
  -H "X-Api-Key: $KEY"
```

---

## Getting Started

### Prerequisites

- Docker 24+
- Docker Compose 2+
- Go 1.25 (for local development)

### Quick Start

```bash
# Clone repository
git clone <repo-url>
cd ocr-passport

# Start all services
make docker-up

# Check health
make health

# View logs
make logs

# Stop everything
make docker-down
```

### Access Points

| Service | URL | Credentials |
|---------|-----|-------------|
| Cabinet Web UI | http://localhost:8084/ | test@example.com / password |
| API Gateway Swagger | http://localhost:8080/swagger/ | - |
| Billing Swagger | http://localhost:8081/swagger/ | - |
| Cabinet Swagger | http://localhost:8084/swagger/ | - |

### Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
# Database
DATABASE_URL=postgres://api_scan:api_scan_secret@localhost:5432/api_scan
BILLING_DATABASE_URL=postgres://billing:billing_secret@localhost:5433/billing_db

# Services
BILLING_URL=http://billing:8080
ORCHESTRATOR_URL=http://orchestrator:8080

# YooKassa (for payments)
YOOKASSA_SECRET_KEY=your_secret_key
YOOKASSA_SHOP_ID=your_shop_id
```

---

## Security

- **API Keys**: bcrypt hashed, never stored in plain text
- **Sessions**: 24h TTL, stored in PostgreSQL
- **Rate Limiting**: Redis-based sliding window (10 RPS default)
- **CORS**: Configured for browser clients
- **Passwords**: bcrypt with default cost
- **No PII Storage**: Images processed in memory only

---

## Monitoring

### View Logs

```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f api-gateway
docker-compose logs -f billing
docker-compose logs -f cabinet
```

### Health Checks

```bash
make health
```

### Log Format

```
[timestamp] METHOD /path - status (bytes) - duration - remote_addr
[Auth] Authenticated org=X key=Y path=/v1/...
[Login] Password verified for user ID: X
```

---

## Project Structure

```
.
├── docker-compose.yml      # Service orchestration
├── Makefile               # Build & test commands
├── README.md              # This file
├── LOCAL_TESTING.md       # Detailed testing guide
├── migrations/            # Database migrations
│   ├── main/             # api_scan DB
│   └── billing/          # billing_db
├── services/             # Microservices
│   ├── api-gateway/      # Entry point
│   ├── billing/          # Payments & subscriptions
│   ├── billing-webhook-yookassa/  # YooKassa webhooks
│   ├── cabinet/          # Personal account + Web UI
│   └── orchestrator/     # OCR processing
├── pkg/                  # Shared libraries
│   ├── logger/           # Logging middleware
│   ├── normalizer/       # OCR result normalization
│   └── ocr/              # OCR providers
├── scripts/              # Utility scripts
│   ├── seed.sql          # Test data
│   └── admin-cli.go      # Admin CLI
└── tests/                # Tests
    └── integration/      # Integration tests
```

---

## Troubleshooting

### "no rows in result set" error

Check that billing account exists:
```bash
docker exec -i api-scan-postgres-billing psql -U billing -d billing_db << 'EOF'
INSERT INTO accounts (id, status) VALUES (1, 'active') ON CONFLICT DO NOTHING;
INSERT INTO balance_snapshots (account_id, real_balance_rub, prepaid_balance_rub) 
VALUES (1, 5000, 1000) ON CONFLICT (account_id) DO UPDATE 
SET real_balance_rub = 5000, prepaid_balance_rub = 1000;
EOF
```

### CORS errors in browser

API Gateway CORS middleware should handle this automatically. If issues persist:
```bash
docker restart api-scan-api-gateway
```

### Database connection errors

Ensure databases are healthy:
```bash
docker-compose ps
make health
```

---

## License

Apache 2.0

## Support

- Telegram: @api_scan_support
- Email: support@api-scan.example.com
