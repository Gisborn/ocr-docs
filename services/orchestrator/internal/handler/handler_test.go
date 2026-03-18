package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"scan.passport.local/api/pkg/normalizer"
	"scan.passport.local/api/services/orchestrator/internal/service"
)

// MockOrchestrator мок для тестирования
type MockOrchestrator struct {
	ProcessFunc func(ctx interface{}, accountID int64, requestID string, imageData []byte) (*service.ProcessResult, error)
}

func (m *MockOrchestrator) Process(ctx interface{}, accountID int64, requestID string, imageData []byte) (*service.ProcessResult, error) {
	if m.ProcessFunc != nil {
		return m.ProcessFunc(ctx, accountID, requestID, imageData)
	}
	return &service.ProcessResult{
		RequestID: requestID,
		Data: &normalizer.NormalizedResult{
			Fields: normalizer.PassportFields{
				LastName:   "Иванов",
				FirstName:  "Иван",
				MiddleName: "Иванович",
				Series:     "4515",
				Number:     "123456",
			},
			Confidences: map[string]float64{
				"last_name":  0.95,
				"first_name": 0.95,
				"series":     0.90,
			},
		},
		Provider:   "yandex",
		Confidence: 0.92,
	}, nil
}

func (m *MockOrchestrator) Stats() map[string]interface{} {
	return map[string]interface{}{}
}

// Adapter для использования мока с реальным Handler
type OrchestratorAdapter struct {
	mock *MockOrchestrator
}

func (a *OrchestratorAdapter) Process(ctx interface{}, accountID int64, requestID string, imageData []byte) (*service.ProcessResult, error) {
	return a.mock.Process(ctx, accountID, requestID, imageData)
}

func (a *OrchestratorAdapter) Stats() map[string]interface{} {
	return a.mock.Stats()
}

func TestHealth(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %s", resp["status"])
	}
}

func TestRecognize_MissingIdempotencyKey(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/recognize", nil)
	rec := httptest.NewRecorder()

	handler.Recognize(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestRecognize_InvalidMethod(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/recognize", nil)
	rec := httptest.NewRecorder()

	handler.Recognize(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestRecognize_JSONRequest(t *testing.T) {
	// Этот тест требует реального orchestrator
	// Для unit-тестов лучше использовать интеграционные тесты
	t.Skip("Integration test - requires real orchestrator")
}

func TestRecognize_MultipartForm(t *testing.T) {
	t.Skip("Integration test - requires real orchestrator")
}

func TestRecognize_UnsupportedContentType(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/recognize", bytes.NewReader([]byte("data")))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Idempotency-Key", "test-req-003")
	rec := httptest.NewRecorder()

	handler.Recognize(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestReadImage_JSON(t *testing.T) {
	handler := NewHandler(nil)

	imageBase64 := base64.StdEncoding.EncodeToString([]byte("test_image"))
	reqBody := map[string]string{
		"image_base64": imageBase64,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/v1/recognize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	data, err := handler.readImage(req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if string(data) != "test_image" {
		t.Errorf("Expected 'test_image', got %s", string(data))
	}
}

func TestReadImage_Multipart(t *testing.T) {
	handler := NewHandler(nil)

	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	part, _ := writer.CreateFormFile("file", "test.jpg")
	part.Write([]byte("multipart_image"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/recognize", &b)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	data, err := handler.readImage(req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if string(data) != "multipart_image" {
		t.Errorf("Expected 'multipart_image', got %s", string(data))
	}
}

func TestRecognizeResponseStructure(t *testing.T) {
	// Проверяем структуру ответа
	resp := RecognizeResponse{
		RequestID:    "test-123",
		DocumentType: "passport_rf",
		Fields: map[string]interface{}{
			"last_name": "Иванов",
		},
		Confidences: map[string]float64{
			"last_name": 0.95,
		},
		Provider: "yandex",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decoded RecognizeResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if decoded.RequestID != resp.RequestID {
		t.Errorf("RequestID mismatch")
	}

	if decoded.DocumentType != resp.DocumentType {
		t.Errorf("DocumentType mismatch")
	}
}
