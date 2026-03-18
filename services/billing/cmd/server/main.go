package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/api-scan/api-scan/services/billing/internal/handler"
	"github.com/api-scan/api-scan/services/billing/internal/repository"
	"github.com/api-scan/api-scan/services/billing/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Конфигурация
	port := getEnv("PORT", "8080")
	databaseURL := getEnv("DATABASE_URL", "postgres://billing:billing_secret@localhost:5433/billing_db")
	yookassaSecret := getEnv("YOOKASSA_SECRET_KEY", "")

	// Подключаемся к БД
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Проверяем соединение
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to database")

	// Создаем репозиторий
	repo := repository.NewPostgresRepository(pool)

	// Создаем сервисы
	billingService := service.NewBillingService(repo)
	subService := service.NewSubscriptionService(repo)
	
	// YooKassa клиент (в MVP можно nil, если не настроены ключи)
	var yookassaClient *service.YooKassaClient
	if yookassaSecret != "" {
		yookassaClient = service.NewYooKassaClient(getEnv("YOOKASSA_SHOP_ID", ""), yookassaSecret)
	}
	paymentService := service.NewPaymentService(repo, yookassaClient, getEnv("WEBHOOK_BASE_URL", ""))

	// Создаем HTTP handler
	httpHandler := handler.NewHandler(billingService, subService, paymentService, yookassaSecret)

	// Настраиваем маршруты
	mux := http.NewServeMux()
	
	// Health
	mux.HandleFunc("/health", httpHandler.Health)
	
	// Accounts
	mux.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			httpHandler.CreateAccount(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/accounts/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		
		// Проверяем sub-path
		if strings.Contains(path, "/reserve") {
			httpHandler.Reserve(w, r)
		} else if strings.Contains(path, "/subscriptions") {
			if strings.Contains(path, "/upgrade") {
				httpHandler.Upgrade(w, r)
			} else {
				httpHandler.CreateSubscription(w, r)
			}
		} else if strings.Contains(path, "/balance") {
			httpHandler.GetBalance(w, r)
		} else if strings.Contains(path, "/payments") {
			if r.Method == http.MethodPost {
				httpHandler.CreatePayment(w, r)
			} else if r.Method == http.MethodGet {
				httpHandler.GetPayment(w, r)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
		}
	})
	
	// Transactions
	mux.HandleFunc("/transactions/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/commit") {
			httpHandler.Commit(w, r)
		} else if strings.HasSuffix(path, "/rollback") {
			httpHandler.Rollback(w, r)
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
		}
	})
	
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

		log.Println("Shutting down gracefully...")
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	log.Printf("Starting billing server on port %s", port)
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
