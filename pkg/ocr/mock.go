package ocr

import (
	"context"
	"fmt"
)

// Mock провайдер для тестирования
type Mock struct {
	name        string
	fixedResult *Result
	fixedError  error
}

// NewMock создает мок-провайдер с фиксированным результатом
func NewMock(name string, result *Result) *Mock {
	return &Mock{
		name:        name,
		fixedResult: result,
	}
}

// NewMockError создает мок-провайдер, который всегда возвращает ошибку
func NewMockError(name string, err error) *Mock {
	return &Mock{
		name:       name,
		fixedError: err,
	}
}

func (m *Mock) Name() string {
	return m.name
}

func (m *Mock) Recognize(ctx context.Context, image []byte) (*Result, error) {
	if m.fixedError != nil {
		return nil, m.fixedError
	}
	
	if m.fixedResult != nil {
		// Возвращаем копию, чтобы не менять оригинал
		result := *m.fixedResult
		return &result, nil
	}
	
	// Дефолтный результат для паспорта РФ
	return &Result{
		DocumentType: "passport_rf",
		RawText:      "ПАСПОРТ РОССИЙСКОЙ ФЕДЕРАЦИИ\nИВАНОВ ИВАН ИВАНОВИЧ\n...",
		Fields: map[string]Field{
			"last_name":       {Value: "Иванов", Confidence: 0.99},
			"first_name":      {Value: "Иван", Confidence: 0.98},
			"middle_name":     {Value: "Иванович", Confidence: 0.97},
			"birth_date":      {Value: "01.01.1990", Confidence: 0.96},
			"series":          {Value: "4510", Confidence: 0.95},
			"number":          {Value: "123456", Confidence: 0.95},
			"issue_date":      {Value: "15.05.2015", Confidence: 0.98},
			"issued_by":       {Value: "Отделом УФМС России по г. Москве", Confidence: 0.97},
			"division_code":   {Value: "770-064", Confidence: 0.96},
		},
	}, nil
}

// MockWithDelay мок с задержкой (для тестирования таймаутов)
type MockWithDelay struct {
	*Mock
	delayMs int
}

func NewMockWithDelay(name string, delayMs int, result *Result) *MockWithDelay {
	return &MockWithDelay{
		Mock:    NewMock(name, result),
		delayMs: delayMs,
	}
}

func (m *MockWithDelay) Recognize(ctx context.Context, image []byte) (*Result, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	
	// В реальном коде здесь была бы задержка
	// time.Sleep(time.Duration(m.delayMs) * time.Millisecond)
	
	return m.Mock.Recognize(ctx, image)
}

// ConfigurableMock мок с настраиваемым поведением
type ConfigurableMock struct {
	name           string
	callCount      int
	results        []Result
	errors         []error
	currentIndex   int
}

func NewConfigurableMock(name string) *ConfigurableMock {
	return &ConfigurableMock{
		name:     name,
		results:  make([]Result, 0),
		errors:   make([]error, 0),
	}
}

func (m *ConfigurableMock) AddResult(result Result) {
	m.results = append(m.results, result)
}

func (m *ConfigurableMock) AddError(err error) {
	m.errors = append(m.errors, err)
}

func (m *ConfigurableMock) Name() string {
	return m.name
}

func (m *ConfigurableMock) Recognize(ctx context.Context, image []byte) (*Result, error) {
	m.callCount++
	
	idx := m.currentIndex
	if idx >= len(m.results)+len(m.errors) {
		// Если вышли за пределы настроенных результатов, возвращаем дефолт
		return NewMock("default", nil).Recognize(ctx, image)
	}
	
	m.currentIndex++
	
	// Чередуем результаты и ошибки
	if idx < len(m.results) {
		result := m.results[idx]
		return &result, nil
	}
	
	errIdx := idx - len(m.results)
	if errIdx < len(m.errors) {
		return nil, m.errors[errIdx]
	}
	
	return nil, fmt.Errorf("unexpected call")
}

func (m *ConfigurableMock) CallCount() int {
	return m.callCount
}
