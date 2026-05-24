# Деплой API-Scan на Ubuntu-сервер

> ⚠️ **Частично устарело**: Демо-стенд использует `jwilder/nginx-proxy` + `letsencrypt-nginx-proxy-companion`, а не Traefik. Этот документ описывает теоретическую production-архитектуру. Актуальная конфигурация demo: `infra/docker/docker-compose.demo.yml`.

Полное руководство по настройке автодеплоя из GitHub Actions на Ubuntu-сервер ( production ).

## Архитектура деплоя

```
GitHub (demo branch)
       │
       ▼ push / workflow_dispatch
GitHub Actions
  ├── Build Docker images
  └── SSH deploy to Ubuntu server
            │
            ▼
      Ubuntu Server (Docker + nginx-proxy)
```

**Технологии:**
- **Docker Compose** — оркестрация контейнеров
- **nginx-proxy** — reverse proxy + автоматические SSL-сертификаты Let's Encrypt
- **GitHub Actions** — CI/CD pipeline (manual trigger)

---

## 1. Подготовка сервера (Ubuntu 22.04+)

### 1.1 Минимальные требования

| Ресурс | Минимум | Рекомендуется |
|--------|---------|---------------|
| CPU | 2 ядра | 4 ядра |
| RAM | 4 GB | 8 GB |
| Disk | 40 GB SSD | 80 GB SSD |
| OS | Ubuntu 22.04 LTS | Ubuntu 24.04 LTS |

### 1.2 Установка Docker и Go

```bash
# Обновление системы
sudo apt update && sudo apt upgrade -y

# Установка Docker
sudo apt install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Добавление пользователя в группу docker
sudo usermod -aG docker $USER
newgrp docker

# Установка Go (для goose миграций)
GO_VERSION=$(curl -s https://go.dev/dl/?mode=json | grep -o '"version": "go[0-9.]*"' | head -1 | grep -o '[0-9.]*')
wget "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
rm "go${GO_VERSION}.linux-amd64.tar.gz"
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
export PATH=$PATH:/usr/local/go/bin

# Установка goose для миграций БД
go install github.com/pressly/goose/v3/cmd/goose@latest
sudo cp ~/go/bin/goose /usr/local/bin/
```

### 1.3 Настройка файрвола

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp   # SSH
sudo ufw allow 80/tcp   # HTTP
sudo ufw allow 443/tcp  # HTTPS
sudo ufw enable
```

### 1.4 Настройка SSH (рекомендуется)

```bash
# Создайте отдельного пользователя для деплоя
sudo adduser deploy
sudo usermod -aG docker deploy

# Скопируйте свой SSH публичный ключ
sudo mkdir -p /home/deploy/.ssh
sudo tee /home/deploy/.ssh/authorized_keys << 'EOF'
ssh-ed25519 AAAAC3NzaC... your-key-comment
EOF
sudo chown -R deploy:deploy /home/deploy/.ssh
sudo chmod 700 /home/deploy/.ssh
sudo chmod 600 /home/deploy/.ssh/authorized_keys
```

---

## 2. Настройка GitHub Container Registry (GHCR)

### 2.1 Создание Personal Access Token (PAT)

На сервере нужно авторизоваться в GHCR, чтобы скачивать приватные образы.

1. Откройте GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Создайте новый токен с правами:
   - `read:packages` (чтение образов)
   - `write:packages` (если пушите вручную)
3. Скопируйте токен

### 2.2 Авторизация на сервере

```bash
# На сервере под пользователем deploy
ssh deploy@your-server-ip

# Логин в GHCR
docker login ghcr.io -u YOUR_GITHUB_USERNAME
# Password: <вставьте PAT>

# Проверка
 cat ~/.docker/config.json | grep ghcr.io
```

---

## 3. Настройка проекта на сервере

### 3.1 Клонирование репозитория

```bash
ssh deploy@your-server-ip

git clone https://github.com/YOUR_USERNAME/ocr-passport.git ~/api-scan
cd ~/api-scan
```

### 3.2 Создание .env файла

```bash
cp infra/docker/.env.example ~/api-scan/.env
nano ~/api-scan/.env
```

**Обязательные переменные:**

```env
# Домены (зарегистрируйте и направьте A-записи на сервер)
API_DOMAIN=api-scan.example.com
CABINET_DOMAIN=cabinet.api-scan.example.com

# SSL
LETSENCRYPT_EMAIL=admin@example.com

# Базы данных (генерируйте надёжные пароли)
POSTGRES_PASSWORD=your_very_secure_password_32_chars
POSTGRES_BILLING_PASSWORD=another_secure_password_32_chars

# Сервисный токен (генерируйте случайную строку)
BILLING_SERVICE_TOKEN=$(openssl rand -hex 32)

# ЮКасса (production credentials)
YOOKASSA_SHOP_ID=your_shop_id
YOOKASSA_SECRET_KEY=live_...

# Yandex Vision
YANDEX_VISION_API_KEY=your_key
YANDEX_FOLDER_ID=your_folder_id
```

### 3.3 Первый ручной деплой

```bash
cd ~/api-scan
chmod +x scripts/deploy.sh

# Логин в GHCR (если ещё не выполнен)
docker login ghcr.io -u YOUR_GITHUB_USERNAME

# Первый запуск
./scripts/deploy.sh
```

---

## 4. Настройка GitHub Actions

### 4.1 Добавление секретов в репозиторий

Откройте репозиторий → Settings → Secrets and variables → Actions → New repository secret

| Секрет | Описание | Пример |
|--------|----------|--------|
| `SSH_HOST` | IP или домен сервера | `185.123.45.67` |
| `SSH_USER` | Пользователь для SSH | `deploy` |
| `SSH_KEY` | Приватный SSH ключ | `-----BEGIN OPENSSH PRIVATE KEY-----...` |
| `SSH_PORT` | Порт SSH (опционально) | `22` |

### 4.2 Environment (опционально, но рекомендуется)

Settings → Environments → New environment → `production`

Добавьте те же секреты в environment. Workflow использует `environment: production`, что даёт:
- Защитные правила (например, требовать ручного подтверждения)
- Отдельный аудит деплоев

### 4.3 Как работает pipeline

При каждом `git push` в `main`:

1. **Build job:**
   - Собирает 5 Docker-образов (api-gateway, billing, billing-webhook-yookassa, cabinet, orchestrator)
   - Пушит их в GHCR с тегом `latest`
   - Использует GitHub Actions cache для ускорения

2. **Deploy job:**
   - Подключается к серверу по SSH
   - Выполняет `docker compose pull`
   - Запускает миграции БД через `goose`
   - Поднимает сервисы (`docker compose up -d`)
   - Очищает неиспользуемые образы

---

## 5. Структура на сервере

```
/home/deploy/api-scan/
├── .env                          # Production переменные окружения
├── infra/
│   └── docker/
│       └── docker-compose.prod.yml
├── scripts/
│   └── deploy.sh                 # Ручной деплой (опционально)
├── migrations/
│   ├── main/                     # SQL-миграции main DB
│   └── billing/                  # SQL-миграции billing DB
└── ... (остальные файлы из git)
```

---

## 6. Управление сервисами

### Просмотр статуса

```bash
cd ~/api-scan
docker compose -f infra/docker/docker-compose.prod.yml ps
```

### Логи

```bash
# Все сервисы
docker compose -f infra/docker/docker-compose.prod.yml logs -f

# Конкретный сервис
docker compose -f infra/docker/docker-compose.prod.yml logs -f cabinet
```

### Перезапуск сервиса

```bash
docker compose -f infra/docker/docker-compose.prod.yml restart cabinet
```

### Ручной деплой (без GitHub Actions)

```bash
cd ~/api-scan
git pull origin main
./scripts/deploy.sh
```

---

## 7. Бэкапы (важно!)

### 7.1 Автоматический бэкап PostgreSQL

Добавьте в `crontab -e`:

```bash
# Ежедневный бэкап в 3:00
0 3 * * * /home/deploy/api-scan/scripts/backup.sh >> /var/log/api-scan-backup.log 2>&1
```

Создайте `scripts/backup.sh`:

```bash
#!/bin/bash
set -euo pipefail

BACKUP_DIR="/home/deploy/backups/api-scan"
DATE=$(date +%Y%m%d_%H%M%S)
mkdir -p "${BACKUP_DIR}"

# Backup main DB
docker exec api-scan-postgres pg_dump -U api_scan api_scan | gzip > "${BACKUP_DIR}/main_${DATE}.sql.gz"

# Backup billing DB
docker exec api-scan-postgres-billing pg_dump -U billing billing_db | gzip > "${BACKUP_DIR}/billing_${DATE}.sql.gz"

# Хранить только последние 7 дней
find "${BACKUP_DIR}" -name "*.sql.gz" -mtime +7 -delete

echo "[${DATE}] Backup completed"
```

### 7.2 Бэкап Let's Encrypt сертификатов

```bash
# Сертификаты хранятся в Docker volume
docker run --rm -v api-scan_traefik_certs:/data -v ~/backups:/backup alpine tar czf /backup/traefik_certs_$(date +%Y%m%d).tar.gz -C /data .
```

---

## 8. Troubleshooting

### 8.1 Образы не скачиваются (401 Unauthorized)

```bash
# Перелогиньтесь в GHCR
docker login ghcr.io -u YOUR_GITHUB_USERNAME

# Проверьте токен
cat ~/.docker/config.json
```

### 8.2 Сертификат Let's Encrypt не выпускается

- Проверьте A-записи доменов (должны указывать на сервер)
- Убедитесь, что порты 80 и 443 открыты
- Проверьте логи Traefik:
  ```bash
  docker logs api-scan-traefik
  ```

### 8.3 Миграции не применяются

```bash
# Запуск вручную
cd ~/api-scan/migrations/main
goose postgres "postgres://api_scan:PASSWORD@localhost:5432/api_scan?sslmode=disable" up

cd ~/api-scan/migrations/billing
goose postgres "postgres://billing:PASSWORD@localhost:5432/billing_db?sslmode=disable" up
```

### 8.4 Сервис не запускается

```bash
# Проверьте переменные окружения
docker compose -f infra/docker/docker-compose.prod.yml config

# Логи сервиса
docker compose -f infra/docker/docker-compose.prod.yml logs --tail 100 cabinet
```

---

## 9. Обновление legal documents

Legal documents (`docs/legal/*.md`) встраиваются в Docker-образ Cabinet на этапе сборки.

**Для обновления на production:**

1. Правьте markdown-файлы в `docs/legal/`
2. Commit + push в `main`
3. GitHub Actions автоматически пересоберёт образ Cabinet и задеплоит

Если нужно обновить срочно без CI:

```bash
# На сервере
nano ~/api-scan/docs/legal/privacy-policy.md
cd ~/api-scan
# Пересобрать только cabinet локально
docker compose -f infra/docker/docker-compose.prod.yml build --no-cache cabinet
docker compose -f infra/docker/docker-compose.prod.yml up -d cabinet
```

---

## 10. Безопасность

| Рекомендация | Статус |
|-------------|--------|
| Использовать отдельного пользователя `deploy` (не root) | ✅ |
| SSH-key аутентификация (отключить password auth) | ✅ |
| firewall (UFW) только 22/80/443 | ✅ |
| Секреты в `.env` (не в репозитории) | ✅ |
| Бэкапы БД ежедневно | ✅ |
| Автоматические SSL-сертификаты | ✅ |
| Rate limiting через Traefik (при необходимости) | Добавить |

---

## Полезные команды

```bash
# Полная пересборка всего
./scripts/deploy.sh

# Остановить всё
docker compose -f infra/docker/docker-compose.prod.yml down

# Остановить с удалением volumes (осторожно!)
docker compose -f infra/docker/docker-compose.prod.yml down -v

# Мониторинг ресурсов
docker stats

# Проверка SSL-сертификата
curl -vI https://api-scan.example.com
```
