// @title API Gateway
// @version 1.0
// @description API Gateway for OCR passport scanning service. Routes requests to Orchestrator, Billing, and other services.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.email support@api-scan.example.com

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-Api-Key

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/api-scan/api-scan/services/api-gateway/docs"
	"github.com/api-scan/api-scan/services/api-gateway/internal/handler"
	"github.com/api-scan/api-scan/services/api-gateway/internal/middleware"
	"github.com/api-scan/api-scan/services/api-gateway/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
	httpSwagger "github.com/swaggo/http-swagger"
)

func main() {
	// Конфигурация
	port := getEnv("PORT", "8080")
	databaseURL := getEnv("DATABASE_URL", "postgres://api_scan:api_scan_secret@localhost:5432/api_scan")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")

	// URL downstream сервисов
	orchestratorURL := getEnv("ORCHESTRATOR_URL", "http://localhost:8083")
	billingURL := getEnv("BILLING_URL", "http://localhost:8081")
	billingWebhookURL := getEnv("BILLING_WEBHOOK_URL", "http://localhost:8082")
	// cabinetURL := getEnv("CABINET_URL", "http://localhost:8084") // Stage 6

	// Подключаемся к БД (main database для API ключей)
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to database")

	// Создаем репозиторий
	repo := repository.NewPostgresRepository(pool)

	// Создаем rate limiter
	rateLimiter := middleware.NewRedisRateLimiter(redisAddr)
	defer rateLimiter.Close()

	// Создаем middleware
	authMiddleware := middleware.NewAuthMiddleware(repo)
	rateLimitMiddleware := middleware.NewRateLimitMiddleware(rateLimiter, 10) // default 10 RPS

	// Создаем handler
	gatewayHandler, err := handler.NewHandler(orchestratorURL, billingURL, "")
	if err != nil {
		log.Fatalf("Failed to create handler: %v", err)
	}

	// Добавляем маршрут для webhook'ов
	webhookHandler, _ := handler.NewHandler("", "", billingWebhookURL)

	// Настраиваем маршруты
	mux := http.NewServeMux()

	// Swagger UI
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)
	
	// Health check (без auth)
	mux.HandleFunc("/health", gatewayHandler.Health)

	// Webhooks (без auth, но с IP whitelist на уровне сервиса)
	mux.HandleFunc("/webhooks/", webhookHandler.ProxyHandler)

	// API endpoints (с auth и rate limit)
	apiHandler := authMiddleware.Handler(
		rateLimitMiddleware.Handler(
			http.HandlerFunc(gatewayHandler.ProxyHandler),
		),
	)
	mux.Handle("/v1/", apiHandler)

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

		log.Println("Shutting down API Gateway gracefully...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	log.Printf("Starting API Gateway on port %s", port)
	log.Printf("Routes:")
	log.Printf("  - /health -> health check")
	log.Printf("  - /v1/* -> authenticated + rate limited")
	log.Printf("  - /webhooks/* -> public (webhook services)")

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
