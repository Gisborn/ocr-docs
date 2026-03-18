FROM golang:1.25-alpine AS builder

WORKDIR /app

# Копируем go.mod и go.sum
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь код
COPY . .

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -o cabinet-service ./services/cabinet/cmd/server

# Финальный образ
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Копируем бинарник из builder
COPY --from=builder /app/cabinet-service .

# Копируем статические файлы (ЛК UI)
COPY --from=builder /app/services/cabinet/pages ./pages

# Открываем порт
EXPOSE 8084

# Запускаем
CMD ["./cabinet-service"]
