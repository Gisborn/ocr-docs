# API-Scan: OCR Service for Russian Passports

> Cloud API service for recognizing Russian Federation passports, designed for integration with 1C and other B2B systems.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Demo Deployment](#demo-deployment)
3. [Technology Stack](#technology-stack)
4. [Services](#services)
5. [API Documentation](#api-documentation)
6. [Business Processes](#business-processes)
7. [Testing](#testing)
8. [Getting Started](#getting-started)
9. [Security](#security)
10. [Monitoring](#monitoring)

---

## Demo Deployment

A demo instance is deployed on a Timeweb VPS (Ubuntu 24.04) and accessible via HTTPS:

| Service | URL |
|---------|-----|
| Landing | https://adocs.ru |
| Cabinet Web UI | https://lk.adocs.ru |
| API Gateway | https://api.adocs.ru |

**Infrastructure:**
- Server: Timeweb microserver, IP `89.223.68.18`
- Reverse proxy: `jwilder/nginx-proxy` + `letsencrypt-nginx-proxy-companion` (Let's Encrypt)
- Docker network `net` shared with nginx-proxy
- All 6 services run in Docker containers via `infra/docker/docker-compose.demo.yml`
- Database migrations applied (main + billing)
- Demo top-ups supported via `mock_payments` table

**Deploy workflow:**
- `.github/workflows/deploy-demo.yml` — manual trigger (`workflow_dispatch`)
- `.github/workflows/deploy.yml.disabled` — production workflow (disabled)

---

## Architecture Overview

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLIENTS                                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐    │
│  │   1C ERP    │  │   Web App   │  │ Mobile App  │  │  Cabinet (Web)  │    │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └────────┬────────┘    │
│                                                                       │
│                              Landing (Web)                              │
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
│ • Normalizer    │  │ • Payments      │  │ • IP Whitelist  │  │ • Balance / Hist│
└────────┬────────┘  └────────┬────────┘  └─────────────────┘  │ • Tariffs / Subs│
                                                               │ • Web UI        │
                                                               └─────────────────┘
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
| Cabinet | 8084 | api_scan_main | Personal account, API keys, billing, Web UI |
| Landing | 8085 | - | Public landing page (adocs.ru) |
| PostgreSQL (main) | 5432 / 15432 (demo) | api_scan | Organizations, users, keys, sessions |
| PostgreSQL (billing) | 5433 / 15433 (demo) | billing_db | Accounts, transactions, billing |
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
- API key management (max 10 keys) with copy-to-clipboard modal
- Balance display and top-up (mock / YooKassa)
- Billing history (payments, charges, prepaid credits)
- Tariff selection and subscription management
- Dark / light theme toggle
- Web UI at `/`
- Swagger docs at `/swagger/`

**Registration:**
- Auto-registration works; no default test account needed
- Registration requires `accepted_terms: true`
- Billing account created automatically with `status: active`

**Endpoints**:
| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/` | GET | No | Web UI (Личный Кабинет) |
| `/api/v1/auth/register` | POST | No | Register |
| `/api/v1/auth/login` | POST | No | Login |
| `/api/v1/auth/logout` | POST | Session | Logout |
| `/api/v1/api-keys` | GET | Session | List keys |
| `/api/v1/api-keys` | POST | Session | Create key (returns full key once) |
| `/api/v1/history` | GET | Session | Billing history (proxy to Billing) |
| `/api/v1/subscription` | GET | Session | Active subscription |
| `/api/v1/subscription` | POST | Session | Create / change subscription |
| `/legal/privacy` | GET | No | Privacy policy (`text/markdown`) |
| `/legal/terms` | GET | No | Terms of service (`text/markdown`) |

---

## API Documentation

Swagger UI доступен для каждого сервиса:
- **API Gateway**: http://localhost:8080/swagger/
- **Billing**: http://localhost:8081/swagger/
- **Cabinet**: http://localhost:8084/swagger/

### Known Limitations

**OCR Confidence (Yandex Vision v2 structured model):**
Yandex Vision API v2 (`model=passport`) возвращает структурированные сущности без confidence по полям. В ответе API-Scan блок `confidences` заполняется дефолтным значением `0.90` для всех полей. Это оценочное значение, не фактическая точность OCR. Для получения реального confidence используйте generic `page` модель (параметр `model=page`) или VK Vision.

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
# Run all unit tests (SQL tests skip automatically if DB is unavailable)
make test

# Run specific service tests
go test ./services/billing/...
go test ./services/api-gateway/...
```

### SQL Repository Tests (with real PostgreSQL)

```bash
# 1. Start PostgreSQL
docker compose -f infra/docker/docker-compose.test.yml up -d postgres postgres-billing

# 2. Run migrations
cd migrations/main && goose up
cd ../billing && goose up

# 3. Run tests with DB connection
export TEST_DATABASE_URL=postgres://api_scan:api_scan_secret@localhost:5432/api_scan
export TEST_BILLING_DATABASE_URL=postgres://billing:billing_secret@localhost:5433/billing_db
go test ./services/...
```

### Integration Tests (with real DB and services)

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
- Full billing flow (register → login → API key → topup → subscription → reserve → commit → history)

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

### Demo URLs

| Service | URL | Description |
|---------|-----|-------------|
| Cabinet Web UI | https://lk.adocs.ru | Личный кабинет (production-like demo) |
| API Gateway | https://api.adocs.ru | API entry point |

### Local Access Points

| Service | URL | Credentials |
|---------|-----|-------------|
| Cabinet Web UI | http://localhost:8084/ | Register a new account |
| API Gateway Swagger | http://localhost:8080/swagger/ | - |
| Billing Swagger | http://localhost:8081/swagger/ | - |
| Cabinet Swagger | http://localhost:8084/swagger/ | - |

### Environment Variables

Copy `.env.example` to `.env` and configure. For demo deployment, see `infra/docker/docker-compose.demo.yml`:

```bash
# Database
DATABASE_URL=postgres://api_scan:api_scan_secret@localhost:15432/api_scan
BILLING_DATABASE_URL=postgres://billing:billing_secret@localhost:15433/billing_db

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
│   ├── landing/          # Public landing page
│   └── orchestrator/     # OCR processing
├── pkg/                  # Shared libraries
│   ├── logger/           # Logging middleware
│   ├── normalizer/       # OCR result normalization
│   ├── ocr/              # OCR providers
│   └── testdb/           # PostgreSQL test helpers
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


