package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/api-scan/api-scan/pkg/normalizer"
	"github.com/api-scan/api-scan/pkg/ocr"
)

// Orchestrator основной сервис обработки
type Orchestrator struct {
	primaryProvider   ocr.Provider
	fallbackProvider  ocr.Provider
	normalizer        *normalizer.Normalizer
	primaryCB         *CircuitBreaker
	fallbackCB        *CircuitBreaker
	confidenceThreshold float64
}

// NewOrchestrator создает новый оркестратор
func NewOrchestrator(
	primary ocr.Provider,
	fallback ocr.Provider,
	cbFailureThreshold int,
	cbTimeout int, // секунды
	confidenceThreshold float64,
) *Orchestrator {
	return &Orchestrator{
		primaryProvider:     primary,
		fallbackProvider:    fallback,
		normalizer:          normalizer.New(),
		primaryCB:           NewCircuitBreaker("primary", cbFailureThreshold, cbTimeout),
		fallbackCB:          NewCircuitBreaker("fallback", cbFailureThreshold, cbTimeout),
		confidenceThreshold: confidenceThreshold,
	}
}

// RecognitionResult результат распознавания
type RecognitionResult struct {
	RequestID      string                 `json:"request_id"`
	DocumentType   string                 `json:"document_type"`
	PassportData   normalizer.PassportData `json:"data"`
	Confidences    normalizer.FieldConfidences `json:"confidences"`
	ProviderUsed   string                 `json:"provider_used"` // yandex или vk
}

// Recognize выполняет полный цикл распознавания
func (o *Orchestrator) Recognize(ctx context.Context, image []byte, documentType string) (*RecognitionResult, error) {
	// Если documentType пустой, используем default
	if documentType == "" {
		documentType = "passport_rf"
	}
	
	// В MVP поддерживаем только passport_rf
	if documentType != "passport_rf" {
		return nil, fmt.Errorf("unsupported document_type: %s (only passport_rf supported in MVP)", documentType)
	}

	// Пытаемся распознать через primary provider
	var ocrResult *ocr.Result
	var providerUsed string
	
	if o.primaryCB.Allow() {
		slog.Debug("trying primary provider", "provider", o.primaryProvider.Name())
		
		result, err := o.primaryProvider.Recognize(ctx, image)
		if err != nil {
			var providerErr *ocr.ProviderError
			if errors.As(err, &providerErr) {
				o.primaryCB.RecordFailure()
				slog.Warn("primary provider failed", 
					"provider", o.primaryProvider.Name(),
					"error", providerErr.Message,
					"type", providerErr.Type)
				
				// Если ошибка retryable, пробуем fallback
				if providerErr.IsRetryable() && o.fallbackCB.Allow() {
					slog.Info("falling back to secondary provider", "provider", o.fallbackProvider.Name())
					result, err = o.fallbackProvider.Recognize(ctx, image)
					if err != nil {
						o.fallbackCB.RecordFailure()
						return nil, fmt.Errorf("both providers failed, last error: %w", err)
					}
					o.fallbackCB.RecordSuccess()
					providerUsed = o.fallbackProvider.Name()
				} else {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			o.primaryCB.RecordSuccess()
			ocrResult = result
			providerUsed = o.primaryProvider.Name()
		}
	} else if o.fallbackCB.Allow() {
		// Primary недоступен (CB open), пробуем fallback
		slog.Info("primary provider circuit breaker open, using fallback")
		
		result, err := o.fallbackProvider.Recognize(ctx, image)
		if err != nil {
			o.fallbackCB.RecordFailure()
			return nil, fmt.Errorf("fallback provider failed: %w", err)
		}
		o.fallbackCB.RecordSuccess()
		ocrResult = result
		providerUsed = o.fallbackProvider.Name()
	} else {
		// Оба провайдера недоступны
		return nil, errors.New("all OCR providers unavailable (circuit breakers open)")
	}
	
	// Нормализуем результат
	normResult, err := o.normalizer.Normalize(ocrResult.RawText)
	if err != nil {
		return nil, fmt.Errorf("normalization failed: %w", err)
	}
	
	// Проверяем confidence
	if !o.checkConfidence(normResult.Confidences) {
		return nil, &LowConfidenceError{
			Confidences: normResult.Confidences,
		}
	}
	
	return &RecognitionResult{
		DocumentType: documentType,
		PassportData: normResult.Data,
		Confidences:  normResult.Confidences,
		ProviderUsed: providerUsed,
	}, nil
}

// checkConfidence проверяет, что все критичные поля имеют достаточный confidence
func (o *Orchestrator) checkConfidence(conf normalizer.FieldConfidences) bool {
	// Проверяем обязательные поля
	criticalFields := []float64{
		conf.LastName,
		conf.FirstName,
		conf.Series,
		conf.Number,
	}
	
	for _, c := range criticalFields {
		if c < o.confidenceThreshold {
			return false
		}
	}
	
	return true
}

// GetStats возвращает статистику работы сервиса
func (o *Orchestrator) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"primary_circuit_breaker":  o.primaryCB.Stats(),
		"fallback_circuit_breaker": o.fallbackCB.Stats(),
	}
}

// LowConfidenceError ошибка низкого confidence
type LowConfidenceError struct {
	Confidences normalizer.FieldConfidences
}

func (e *LowConfidenceError) Error() string {
	return "recognition confidence below threshold"
}
