package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"scan.passport.local/api/services/billing-webhook-yookassa/internal/handler"
	"scan.passport.local/api/services/billing-webhook-yookassa/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Конфигурация
	port := getEnv("PORT", "8081")
	databaseURL := getEnv("DATABASE_URL", "postgres://billing:billing_secret@localhost:5433/billing_db")
	yookassaSecret := getEnv("YOOKASSA_SECRET_KEY", "")
	
	// IP Whitelist для ЮКассы (разделенные запятыми)
	ipWhitelist := getEnv("IP_WHITELIST", "185.71.76.0/27,185.71.77.0/27,77.75.153.0/25")

	// Подключаемся к БД billing
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Проверяем соединение
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to billing database")

	// Создаем репозиторий (shared с billing service)
	repo := repository.NewPostgresRepository(pool)

	// Создаем HTTP handler
	httpHandler := handler.NewHandler(repo, yookassaSecret, ipWhitelist)

	// Настраиваем маршруты
	mux := http.NewServeMux()
	
	// Health
	mux.HandleFunc("/health", httpHandler.Health)
	
	// Webhooks
	mux.HandleFunc("/webhooks/yookassa", httpHandler.YookassaWebhook)

	// Создаем сервер
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		<-sigChan

		log.Println("Shutting down billing-webhook gracefully...")
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	log.Printf("Starting billing-webhook server on port %s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
