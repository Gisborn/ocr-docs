package ocr

import (
	"context"
	"fmt"
)

// Provider интерфейс для OCR-провайдеров
type Provider interface {
	// Recognize распознает текст на изображении
	// Возвращает результат с распознанными полями и confidence
	Recognize(ctx context.Context, image []byte) (*Result, error)
	
	// Name возвращает имя провайдера (для логирования и метрик)
	Name() string
}

// Result результат распознавания
type Result struct {
	// RawText сырой распознанный текст (для отладки)
	RawText string `json:"raw_text"`
	
	// Fields распознанные поля документа
	Fields map[string]Field `json:"fields"`
	
	// DocumentType тип документа (определяется или передается)
	DocumentType string `json:"document_type"`
}

// Field представляет одно распознанное поле
type Field struct {
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// Confidence возвращает минимальный confidence среди всех полей
// Используется для принятия решения о fallback
func (r *Result) Confidence() float64 {
	if len(r.Fields) == 0 {
		return 0
	}
	
	minConf := 1.0
	for _, field := range r.Fields {
		if field.Confidence < minConf {
			minConf = field.Confidence
		}
	}
	return minConf
}

// ProviderError ошибка OCR-провайдера
type ProviderError struct {
	Provider string
	Type     ErrorType
	Message  string
	Cause    error
}

type ErrorType string

const (
	ErrorTypeNetwork    ErrorType = "network"     // Ошибка сети, таймаут
	ErrorTypeAPI        ErrorType = "api"         // Ошибка API (5xx)
	ErrorTypeAuth       ErrorType = "auth"        // Ошибка авторизации
	ErrorTypeRateLimit  ErrorType = "rate_limit"  // Превышен rate limit
	ErrorTypeInvalid    ErrorType = "invalid"     // Невалидный запрос
	ErrorTypeUnknown    ErrorType = "unknown"     // Неизвестная ошибка
)

func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("ocr provider %s error [%s]: %s: %v", e.Provider, e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("ocr provider %s error [%s]: %s", e.Provider, e.Type, e.Message)
}

func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// IsRetryable возвращает true, если ошибку можно повторить (fallback)
func (e *ProviderError) IsRetryable() bool {
	switch e.Type {
	case ErrorTypeNetwork, ErrorTypeAPI, ErrorTypeRateLimit:
		return true
	default:
		return false
	}
}
