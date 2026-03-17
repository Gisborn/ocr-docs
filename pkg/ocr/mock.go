package ocr

import (
	"context"
	"math/rand"
)

// MockProvider мок OCR провайдер для тестирования
type MockProvider struct {
	name      string
	failRate  float64 // Вероятность ошибки (0-1)
	minConf   float64 // Минимальный confidence
	maxConf   float64 // Максимальный confidence
}

// NewMockProvider создает мок провайдер
func NewMockProvider() *MockProvider {
	return &MockProvider{
		name:     "mock",
		failRate: 0,
		minConf:  0.90,
		maxConf:  0.99,
	}
}

// NewMockProviderWithFailure создает мок с заданной вероятностью ошибок
func NewMockProviderWithFailure(name string, failRate float64) *MockProvider {
	return &MockProvider{
		name:     name,
		failRate: failRate,
		minConf:  0.90,
		maxConf:  0.99,
	}
}

func (m *MockProvider) Name() string {
	return m.name
}

// Recognize возвращает мок-результат
func (m *MockProvider) Recognize(ctx context.Context, image []byte) (*Result, error) {
	// Симулируем ошибку
	if rand.Float64() < m.failRate {
		return nil, &ProviderError{
			Provider: m.name,
			Type:     ErrorTypeAPI,
			Message:  "mock failure",
		}
	}

	// Генерируем случайный confidence
	confidence := m.minConf + rand.Float64()*(m.maxConf-m.minConf)

	return &Result{
		RawText:      "МОК ПАСПОРТ\nИВАНОВ ИВАН ИВАНОВИЧ\n01.01.1990\n4515 123456\n15.05.2015\nОТДЕЛОМ УФМС РОССИИ\n770-064",
		DocumentType: "passport_rf",
		Fields: map[string]Field{
			"last_name":     {Value: "ИВАНОВ", Confidence: confidence},
			"first_name":    {Value: "ИВАН", Confidence: confidence},
			"middle_name":   {Value: "ИВАНОВИЧ", Confidence: confidence},
			"birth_date":    {Value: "01.01.1990", Confidence: confidence},
			"series":        {Value: "4515", Confidence: confidence},
			"number":        {Value: "123456", Confidence: confidence},
			"issue_date":    {Value: "15.05.2015", Confidence: confidence},
			"issued_by":     {Value: "ОТДЕЛОМ УФМС РОССИИ", Confidence: confidence},
			"division_code": {Value: "770-064", Confidence: confidence},
		},
	}, nil
}
