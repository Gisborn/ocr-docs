package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/api-scan/api-scan/services/orchestrator/internal/service"
)

// Handler HTTP обработчики
type Handler struct {
	orchestrator *service.FullOrchestrator
}

// NewHandler создает новый handler
func NewHandler(orchestrator *service.FullOrchestrator) *Handler {
	return &Handler{orchestrator: orchestrator}
}

// Health проверка здоровья сервиса
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// Stats статистика Circuit Breakers
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.orchestrator.Stats()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// RecognizeRequest запрос на распознавание
type RecognizeRequest struct {
	AccountID   int64  `json:"account_id"`
	RequestID   string `json:"request_id"`
	ImageBase64 string `json:"image_base64"`
}

// RecognizeResponse ответ на распознавание
type RecognizeResponse struct {
	RequestID   string                 `json:"request_id"`
	DocumentType string                `json:"document_type"`
	Fields      map[string]interface{} `json:"fields"`
	Confidences map[string]float64     `json:"confidences"`
	Provider    string                 `json:"provider"`
}

// Recognize обрабатывает запрос на распознавание паспорта
func (h *Handler) Recognize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Читаем тело запроса
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Парсим JSON
	var req RecognizeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Валидация
	if req.AccountID == 0 {
		http.Error(w, "account_id required", http.StatusBadRequest)
		return
	}
	if req.RequestID == "" {
		http.Error(w, "request_id required", http.StatusBadRequest)
		return
	}
	if req.ImageBase64 == "" {
		http.Error(w, "image_base64 required", http.StatusBadRequest)
		return
	}

	// Декодируем base64 изображение (упрощенно - ожидаем raw bytes)
	// В реальности здесь должна быть base64 декодирование
	imageData := []byte(req.ImageBase64)

	// Вызываем orchestrator
	result, err := h.orchestrator.Process(r.Context(), req.AccountID, req.RequestID, imageData)
	if err != nil {
		switch err {
		case service.ErrInsufficientBalance:
			http.Error(w, "Insufficient balance", http.StatusPaymentRequired)
		case service.ErrBillingUnavailable:
			http.Error(w, "Billing service unavailable", http.StatusServiceUnavailable)
		default:
			http.Error(w, fmt.Sprintf("Processing failed: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Формируем ответ
	resp := RecognizeResponse{
		RequestID:    result.RequestID,
		DocumentType: "passport_rf",
		Fields: map[string]interface{}{
			"last_name":     result.Data.Fields.LastName,
			"first_name":    result.Data.Fields.FirstName,
			"middle_name":   result.Data.Fields.MiddleName,
			"birth_date":    result.Data.Fields.BirthDate,
			"series":        result.Data.Fields.Series,
			"number":        result.Data.Fields.Number,
			"issue_date":    result.Data.Fields.IssueDate,
			"issued_by":     result.Data.Fields.IssuedBy,
			"division_code": result.Data.Fields.DivisionCode,
		},
		Confidences: result.Data.Confidences,
		Provider:    result.Provider,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
