package config

import (
	"os"
	"strconv"
	"time"
)

// Config конфигурация сервиса
type Config struct {
	// HTTP
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	
	// OCR
	OCRPrimaryProvider   string  // yandex или vk
	OCRFallbackProvider  string  // vk или mock
	OCRConfidenceThreshold float64 // 0.0 - 1.0
	
	// Yandex Vision
	YandexVisionAPIKey   string
	YandexVisionFolderID string
	
	// VK Vision
	VKVisionAPIKey string
	
	// Circuit Breaker
	CBFailureThreshold int           // Количество ошибок для открытия
	CBTimeout          time.Duration // Время в открытом состоянии
	
	// PDF Conversion
	PDFMaxSizeMB int
}

// Load загружает конфигурацию из переменных окружения
func Load() *Config {
	return &Config{
		Port:         getEnv("PORT", "8080"),
		ReadTimeout:  getDuration("READ_TIMEOUT", 10*time.Second),
		WriteTimeout: getDuration("WRITE_TIMEOUT", 30*time.Second),
		
		OCRPrimaryProvider:    getEnv("OCR_PRIMARY_PROVIDER", "yandex"),
		OCRFallbackProvider:   getEnv("OCR_FALLBACK_PROVIDER", "vk"),
		OCRConfidenceThreshold: getFloat("OCR_CONFIDENCE_THRESHOLD", 0.80),
		
		YandexVisionAPIKey:   getEnv("YANDEX_VISION_API_KEY", ""),
		YandexVisionFolderID: getEnv("YANDEX_VISION_FOLDER_ID", ""),
		
		VKVisionAPIKey: getEnv("VK_VISION_API_KEY", ""),
		
		CBFailureThreshold: getInt("CB_FAILURE_THRESHOLD", 5),
		CBTimeout:          getDuration("CB_TIMEOUT", 30*time.Second),
		
		PDFMaxSizeMB: getInt("PDF_MAX_SIZE_MB", 10),
	}
}

// IsDevelopment проверяет, что сервис запущен в dev-режиме
func (c *Config) IsDevelopment() bool {
	return os.Getenv("ENV") != "production"
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func getDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
