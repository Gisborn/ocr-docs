package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"scan.passport.local/api/pkg/ocr"
	"scan.passport.local/api/services/orchestrator/internal/handler"
	"scan.passport.local/api/services/orchestrator/internal/service"
)

func main() {
	// Читаем конфигурацию из env
	port := getEnv("PORT", "8080")

	// OCR провайдеры
	yandexAPIKey := getEnv("YANDEX_VISION_API_KEY", "")
	yandexFolderID := getEnv("YANDEX_FOLDER_ID", "")
	vkAPIKey := getEnv("VK_VISION_API_KEY", "")
	vkFolderID := getEnv("VK_FOLDER_ID", "")

	// Yandex Vision v2 конфигурация
	yandexUseV2 := getEnvBool("YANDEX_VISION_USE_V2", true)
	yandexModel := getEnv("YANDEX_VISION_MODEL", "passport")

	// Billing Service
	billingURL := getEnv("BILLING_API_URL", "http://billing:8080")
	billingToken := getEnv("BILLING_SERVICE_TOKEN", "")

	// Порог confidence (по умолчанию 0.80)
	confidenceThreshold := 0.80
	if thresholdStr := getEnv("OCR_CONFIDENCE_THRESHOLD", ""); thresholdStr != "" {
		if threshold, err := strconv.ParseFloat(thresholdStr, 64); err == nil {
			confidenceThreshold = threshold
		}
	}

	// Создаем OCR провайдеры
	var primary, fallback ocr.Provider

	if yandexAPIKey != "" {
		if yandexUseV2 {
			model := ocr.DocumentModel(yandexModel)
			if !model.IsValid() {
				log.Printf("WARNING: Invalid YANDEX_VISION_MODEL=%q, using default 'passport'", yandexModel)
				model = ocr.ModelPassportRF
			}
			primary = ocr.NewYandexVisionV2(yandexAPIKey, model)
			log.Printf("Using Yandex Vision v2 (model=%s) as primary OCR provider", model)
		} else if yandexFolderID != "" {
			primary = ocr.NewYandexVision(yandexAPIKey, yandexFolderID)
			log.Println("Using Yandex Vision v1 (legacy) as primary OCR provider")
		} else {
			log.Println("WARNING: Yandex Vision v1 requires YANDEX_FOLDER_ID, using mock")
			primary = ocr.NewMockProvider()
		}
	} else {
		log.Println("WARNING: Yandex Vision not configured, using mock")
		primary = ocr.NewMockProvider()
	}

	if vkAPIKey != "" && vkFolderID != "" {
		fallback = ocr.NewVKVision(vkAPIKey, vkFolderID)
		log.Println("Using VK Vision as fallback OCR provider")
	} else {
		log.Println("WARNING: VK Vision not configured, using mock")
		fallback = ocr.NewMockProvider()
	}

	// Создаем Billing клиент
	billingClient := service.NewBillingClient(billingURL, billingToken)

	// Создаем Orchestrator
	orchestrator := service.NewFullOrchestrator(
		billingClient,
		primary,
		fallback,
		confidenceThreshold,
	)

	// Создаем HTTP handler
	httpHandler := handler.NewHandler(orchestrator)

	// Настраиваем HTTP сервер
	mux := http.NewServeMux()
	mux.HandleFunc("/health", httpHandler.Health)
	mux.HandleFunc("/v1/recognize", httpHandler.Recognize)
	mux.HandleFunc("/stats", httpHandler.Stats)

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

	log.Printf("Starting orchestrator server on port %s", port)
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

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}
