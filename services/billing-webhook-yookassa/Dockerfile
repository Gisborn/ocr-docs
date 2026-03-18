FROM golang:1.22-alpine AS builder

WORKDIR /app

# Копируем go.mod и go.sum
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь код
COPY . .

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -o billing-webhook-yookassa ./services/billing-webhook-yookassa/cmd/server

# Финальный образ
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Копируем бинарник из builder
COPY --from=builder /app/billing-webhook-yookassa .

# Открываем порт
EXPOSE 8080

# Запускаем
CMD ["./billing-webhook-yookassa"]
