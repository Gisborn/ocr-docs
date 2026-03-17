package service

import (
	"context"
	"fmt"
	"time"

	"github.com/api-scan/api-scan/pkg/normalizer"
	"github.com/api-scan/api-scan/pkg/ocr"
)

// FullOrchestrator полный orchestrator с Billing, OCR fallback и Circuit Breaker
type FullOrchestrator struct {
	billing     *BillingClient
	primary     ocr.Provider
	fallback    ocr.Provider
	cbPrimary   *CircuitBreaker
	cbFallback  *CircuitBreaker
	confidenceThreshold float64
}

// NewFullOrchestrator создает полный orchestrator
func NewFullOrchestrator(
	billing *BillingClient,
	primary, fallback ocr.Provider,
	confidenceThreshold float64,
) *FullOrchestrator {
	return &FullOrchestrator{
		billing:             billing,
		primary:             primary,
		fallback:            fallback,
		cbPrimary:           NewCircuitBreaker("yandex-vision", 5, 30*time.Second),
		cbFallback:          NewCircuitBreaker("vk-vision", 5, 30*time.Second),
		confidenceThreshold: confidenceThreshold,
	}
}

// ProcessResult результат обработки
type ProcessResult struct {
	Data        *normalizer.NormalizedResult
	RequestID   string
	FromCache   bool
	Provider    string
	Confidence  float64
}

// Process обрабатывает изображение полным циклом: Reserve → OCR → Commit/Rollback
func (o *FullOrchestrator) Process(
	ctx context.Context,
	accountID int64,
	requestID string,
	image []byte,
) (*ProcessResult, error) {

	// Шаг 1: Резервирование средств
	reserveResp, err := o.billing.Reserve(ctx, accountID, &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: requestID,
	})
	if err != nil {
		return nil, fmt.Errorf("reserve failed: %w", err)
	}

	// Шаг 2: OCR с fallback
	ocrResult, provider, err := o.doOCR(ctx, image)
	if err != nil {
		// Rollback при ошибке OCR
		if rbErr := o.billing.Rollback(ctx, reserveResp.TransactionID, "ocr_failed"); rbErr != nil {
			// Логируем, но возвращаем основную ошибку
			fmt.Printf("rollback failed: %v\n", rbErr)
		}
		return nil, fmt.Errorf("ocr failed: %w", err)
	}

	// Проверяем confidence
	if ocrResult.Confidence() < o.confidenceThreshold {
		// Rollback при низком confidence
		if rbErr := o.billing.Rollback(ctx, reserveResp.TransactionID, "low_confidence"); rbErr != nil {
			fmt.Printf("rollback failed: %v\n", rbErr)
		}
		return nil, fmt.Errorf("confidence too low: %.2f < %.2f", ocrResult.Confidence(), o.confidenceThreshold)
	}

	// Шаг 3: Нормализация
	rawFields := make(map[string]string)
	rawConfidences := make(map[string]float64)
	for key, field := range ocrResult.Fields {
		rawFields[key] = field.Value
		rawConfidences[key] = field.Confidence
	}

	normalized := normalizer.NormalizePassport(rawFields, rawConfidences)

	// Шаг 4: Commit
	if err := o.billing.Commit(ctx, reserveResp.TransactionID); err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	return &ProcessResult{
		Data:       normalized,
		RequestID:  requestID,
		Provider:   provider,
		Confidence: ocrResult.Confidence(),
	}, nil
}

// doOCR выполняет OCR с fallback и Circuit Breaker
func (o *FullOrchestrator) doOCR(ctx context.Context, image []byte) (*ocr.Result, string, error) {
	// Пробуем primary если Circuit Breaker позволяет
	if o.cbPrimary.Allow() {
		result, err := o.primary.Recognize(ctx, image)
		if err == nil {
			o.cbPrimary.RecordSuccess()
			return result, o.primary.Name(), nil
		}

		// Проверяем retryable ошибку
		if providerErr, ok := err.(*ocr.ProviderError); ok && providerErr.IsRetryable() {
			o.cbPrimary.RecordFailure()
		}
	}

	// Fallback на второго провайдера
	if o.cbFallback.Allow() {
		result, err := o.fallback.Recognize(ctx, image)
		if err == nil {
			o.cbFallback.RecordSuccess()
			return result, o.fallback.Name(), nil
		}

		if providerErr, ok := err.(*ocr.ProviderError); ok && providerErr.IsRetryable() {
			o.cbFallback.RecordFailure()
		}
		return nil, "", err
	}

	return nil, "", fmt.Errorf("both providers unavailable")
}

// Stats возвращает статистику Circuit Breakers
func (o *FullOrchestrator) Stats() map[string]interface{} {
	return map[string]interface{}{
		"primary_circuit_breaker":  o.cbPrimary.Stats(),
		"fallback_circuit_breaker": o.cbFallback.Stats(),
	}
}
