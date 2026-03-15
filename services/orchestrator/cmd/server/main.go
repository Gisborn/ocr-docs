package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/api-scan/api-scan/pkg/ocr"
	"github.com/api-scan/api-scan/services/orchestrator/internal/config"
	"github.com/api-scan/api-scan/services/orchestrator/internal/handler"
	"github.com/api-scan/api-scan/services/orchestrator/internal/service"
)

func main() {
	// Настраиваем логирование
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	
	// Загружаем конфигурацию
	cfg := config.Load()
	
	slog.Info("starting orchestrator service",
		"port", cfg.Port,
		"primary_provider", cfg.OCRPrimaryProvider,
		"fallback_provider", cfg.OCRFallbackProvider,
	)
	
	// Создаем OCR провайдеры
	var primaryProvider, fallbackProvider ocr.Provider
	
	switch cfg.OCRPrimaryProvider {
	case "yandex":
		if cfg.YandexVisionAPIKey == "" {
			slog.Warn("YANDEX_VISION_API_KEY not set, using mock provider")
			primaryProvider = ocr.NewMock("yandex-mock", nil)
		} else {
			primaryProvider = ocr.NewYandexVision(cfg.YandexVisionAPIKey, cfg.YandexVisionFolderID)
		}
	case "vk":
		if cfg.VKVisionAPIKey == "" {
			slog.Warn("VK_VISION_API_KEY not set, using mock provider")
			primaryProvider = ocr.NewMock("vk-mock", nil)
		} else {
			primaryProvider = ocr.NewVKVision(cfg.VKVisionAPIKey)
		}
	case "mock":
		primaryProvider = ocr.NewMock("mock", nil)
	default:
		slog.Error("unknown primary provider", "provider", cfg.OCRPrimaryProvider)
		os.Exit(1)
	}
	
	switch cfg.OCRFallbackProvider {
	case "yandex":
		if cfg.YandexVisionAPIKey == "" {
			fallbackProvider = ocr.NewMock("yandex-mock", nil)
		} else {
			fallbackProvider = ocr.NewYandexVision(cfg.YandexVisionAPIKey, cfg.YandexVisionFolderID)
		}
	case "vk":
		if cfg.VKVisionAPIKey == "" {
			fallbackProvider = ocr.NewMock("vk-mock", nil)
		} else {
			fallbackProvider = ocr.NewVKVision(cfg.VKVisionAPIKey)
		}
	case "mock":
		fallbackProvider = ocr.NewMock("mock", nil)
	default:
		fallbackProvider = ocr.NewMock("fallback-mock", nil)
	}
	
	// Создаем оркестратор
	orchestrator := service.NewOrchestrator(
		primaryProvider,
		fallbackProvider,
		cfg.CBFailureThreshold,
		int(cfg.CBTimeout.Seconds()),
		cfg.OCRConfidenceThreshold,
	)
	
	// Создаем handler
	h := handler.New(orchestrator)
	
	// Настраиваем mux
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/recognize", h.RecognizeHandler)
	mux.HandleFunc("/health", h.HealthHandler)
	mux.HandleFunc("/stats", h.StatsHandler)
	
	// Создаем сервер
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}
	
	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	
	// Запускаем сервер в горутине
	go func() {
		slog.Info("server started", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()
	
	// Ждем сигнала завершения
	<-quit
	slog.Info("shutting down server...")
	
	// Graceful shutdown с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
		os.Exit(1)
	}
	
	slog.Info("server stopped")
}
