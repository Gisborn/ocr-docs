// @title Cabinet Service API
// @version 1.0
// @description Personal cabinet service for OCR passport scanning. Manages organizations, users, API keys.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.email support@api-scan.example.com

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8084
// @BasePath /

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

	_ "scan.passport.local/api/services/cabinet/docs"
	"scan.passport.local/api/services/cabinet/internal/handler"
	"scan.passport.local/api/services/cabinet/internal/middleware"
	"scan.passport.local/api/services/cabinet/internal/repository"
	"scan.passport.local/api/services/cabinet/internal/service"
	"scan.passport.local/api/pkg/logger"
	"github.com/jackc/pgx/v5/pgxpool"
	httpSwagger "github.com/swaggo/http-swagger"
)

func main() {
	// Конфигурация
	port := getEnv("PORT", "8084")
	databaseURL := getEnv("DATABASE_URL", "postgres://api_scan:api_scan_secret@localhost:5432/api_scan")
	billingURL := getEnv("BILLING_URL", "http://billing:8080")
	billingToken := getEnv("BILLING_SERVICE_TOKEN", "")

	// Подключаемся к БД
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

	// Создаем сервисы
	authService := service.NewAuthService(repo, billingURL, billingToken)
	apiKeyService := service.NewAPIKeyService(repo)

	// Создаем middleware
	authMiddleware := middleware.NewAuthMiddleware(repo)

	// Создаем HTTP handler
	httpHandler := handler.NewHandler(authService, apiKeyService)

	// Настраиваем маршруты
	mux := http.NewServeMux()

	// Static files (Личный Кабинет UI)
	pagesDir := getEnv("PAGES_DIR", "./pages")
	fs := http.FileServer(http.Dir(pagesDir))
	mux.Handle("/", fs)

	// Swagger UI
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)

	// Health
	mux.HandleFunc("/health", httpHandler.Health)

	// Public API (без auth)
	mux.HandleFunc("/api/v1/auth/register", httpHandler.Register)
	mux.HandleFunc("/api/v1/auth/login", httpHandler.Login)

	// Protected API (с auth)
	protected := authMiddleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/api/v1/auth/verify":
			httpHandler.Verify(w, r)
		case path == "/api/v1/auth/logout":
			httpHandler.Logout(w, r)
		case path == "/api/v1/api-keys":
			if r.Method == http.MethodGet {
				httpHandler.ListAPIKeys(w, r)
			} else if r.Method == http.MethodPost {
				httpHandler.CreateAPIKey(w, r)
			} else {
				http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			}
		case strings.HasPrefix(path, "/api/v1/api-keys/"):
			httpHandler.RevokeAPIKey(w, r)
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	mux.Handle("/api/v1/", protected)

	// Оборачиваем в logging и CORS middleware
	loggedMux := logger.LoggingMiddleware(mux)
	corsMux := middleware.CORS(loggedMux)
	
	// Создаем сервер
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      corsMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		<-sigChan

		log.Println("Shutting down cabinet service gracefully...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	log.Printf("Starting Cabinet Service on port %s", port)
	log.Printf("Routes:")
	log.Printf("  - /health -> health check")
	log.Printf("  - /api/v1/auth/register -> register")
	log.Printf("  - /api/v1/auth/login -> login")
	log.Printf("  - /api/v1/auth/logout -> logout (protected)")
	log.Printf("  - /api/v1/api-keys -> API keys management (protected)")

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
