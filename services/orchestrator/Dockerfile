# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Устанавливаем зависимости для сборки
RUN apk add --no-cache git

# Копируем go.mod и go.sum
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь код
COPY . .

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o orchestrator ./services/orchestrator/cmd/server

# Final stage
FROM alpine:latest

# Устанавливаем ca-certificates для HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Копируем бинарник из builder
COPY --from=builder /app/orchestrator .

# Копируем poppler-utils для конвертации PDF (если нужно)
# RUN apk add --no-cache poppler-utils

# Открываем порт
EXPOSE 8080

# Запускаем
CMD ["./orchestrator"]
