package service

import (
	"context"
	"fmt"
	"time"

	"scan.passport.local/api/pkg/normalizer"
	"scan.passport.local/api/pkg/ocr"
)

// OCRProvider интерфейс OCR провайдера (alias для ocr.Provider)
type OCRProvider = ocr.Provider

// Orchestrator основной сервис обработки
type Orchestrator struct {
	primary   OCRProvider
	fallback  OCRProvider
	normalizer *PassportNormalizer
}

// NewOrchestrator создает новый orchestrator с primary и fallback провайдерами
func NewOrchestrator(primary, fallback OCRProvider) *Orchestrator {
	return &Orchestrator{
		primary:    primary,
		fallback:   fallback,
		normalizer: &PassportNormalizer{},
	}
}

// ProcessRequest обрабатывает запрос на распознавание
// Возвращает нормализованный результат или ошибку
func (o *Orchestrator) ProcessRequest(ctx context.Context, image []byte) (*normalizer.NormalizedResult, error) {
	// Пробуем primary провайдер
	result, err := o.tryProvider(ctx, o.primary, image)
	if err == nil {
		return result, nil
	}

	// Проверяем можно ли делать fallback
	if providerErr, ok := err.(*ocr.ProviderError); ok && providerErr.IsRetryable() {
		// Fallback на второго провайдера
		result, err = o.tryProvider(ctx, o.fallback, image)
		if err == nil {
			return result, nil
		}
	}

	// Если оба провайдера не сработали
	return nil, fmt.Errorf("ocr failed: %w", err)
}

// tryProvider пытается распознать одним провайдером
func (o *Orchestrator) tryProvider(ctx context.Context, provider OCRProvider, image []byte) (*normalizer.NormalizedResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rawResult, err := provider.Recognize(ctx, image)
	if err != nil {
		return nil, err
	}

	// Конвертируем поля OCR в строки для нормализатора
	rawFields := make(map[string]string)
	rawConfidences := make(map[string]float64)

	for key, field := range rawResult.Fields {
		rawFields[key] = field.Value
		rawConfidences[key] = field.Confidence
	}

	// Нормализуем результат
	normalized := normalizer.NormalizePassport(rawFields, rawConfidences)

	return normalized, nil
}

// PassportNormalizer обертка для нормализации
type PassportNormalizer struct{}

// Normalize нормализует сырые поля
func (n *PassportNormalizer) Normalize(fields map[string]string, confidences map[string]float64) *normalizer.NormalizedResult {
	return normalizer.NormalizePassport(fields, confidences)
}

// ResultWithError результат с ошибкой для fallback
type ResultWithError struct {
	Result *normalizer.NormalizedResult
	Error  error
	From   string // "primary" или "fallback"
}
