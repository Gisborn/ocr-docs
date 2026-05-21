package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"scan.passport.local/api/services/orchestrator/internal/service"
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

// RecognizeRequest запрос на распознавание (JSON API)
type RecognizeRequest struct {
	AccountID   int64  `json:"account_id"`
	RequestID   string `json:"request_id"`
	ImageBase64 string `json:"image_base64"`
}

// MaxUploadSize максимальный размер загружаемого файла (10 MB)
const MaxUploadSize = 10 * 1024 * 1024

// RecognizeResponse ответ на распознавание
type RecognizeResponse struct {
	RequestID   string                 `json:"request_id"`
	DocumentType string                `json:"document_type"`
	Fields      map[string]interface{} `json:"fields"`
	Confidences map[string]float64     `json:"confidences"`
	Provider    string                 `json:"provider"`
}

// Recognize обрабатывает запрос на распознавание паспорта
// Поддерживает content-type:
// - application/json: {"account_id": 123, "request_id": "...", "image_base64": "..."}
// - multipart/form-data: file upload + idempotency-key header
func (h *Handler) Recognize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Получаем idempotency key из заголовка
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = r.Header.Get("X-Request-ID")
	}
	if idempotencyKey == "" {
		http.Error(w, `{"error":"idempotency key required","code":"MISSING_IDEMPOTENCY_KEY"}`, http.StatusBadRequest)
		return
	}

	// Получаем account_id из контекста (установлен API Gateway) или из запроса
	accountID := h.getAccountID(r)
	if accountID == 0 {
		http.Error(w, `{"error":"account_id required","code":"MISSING_ACCOUNT"}`, http.StatusBadRequest)
		return
	}

	// Читаем изображение
	imageData, err := h.readImage(r)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s","code":"INVALID_IMAGE"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Проверяем размер
	if len(imageData) > MaxUploadSize {
		http.Error(w, `{"error":"file too large","code":"FILE_TOO_LARGE"}`, http.StatusRequestEntityTooLarge)
		return
	}

	// Вызываем orchestrator
	result, err := h.orchestrator.Process(r.Context(), accountID, idempotencyKey, imageData)
	if err != nil {
		switch err {
		case service.ErrInsufficientBalance:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "insufficient balance",
				"code":    "PAYMENT_REQUIRED",
				"balance": 0,
			})
		case service.ErrBillingUnavailable:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "billing service unavailable",
				"code":  "BILLING_UNAVAILABLE",
			})
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": fmt.Sprintf("processing failed: %v", err),
				"code":  "PROCESSING_ERROR",
			})
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
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// getAccountID получает account_id из контекста или query параметра
func (h *Handler) getAccountID(r *http.Request) int64 {
	// Проверяем контекст (установлен API Gateway)
	if orgID := r.Context().Value("organization_id"); orgID != nil {
		if id, ok := orgID.(int64); ok {
			return id
		}
	}

	// Fallback на заголовок от API Gateway
	if orgIDHeader := r.Header.Get("X-Organization-ID"); orgIDHeader != "" {
		if id, err := strconv.ParseInt(orgIDHeader, 10, 64); err == nil {
			return id
		}
	}

	// Fallback на query параметр (для тестирования)
	accountIDStr := r.URL.Query().Get("account_id")
	if accountIDStr != "" {
		if id, err := strconv.ParseInt(accountIDStr, 10, 64); err == nil {
			return id
		}
	}

	return 0
}

// readImage читает изображение из запроса
// Поддерживает: multipart/form-data (file поле) и application/json (base64)
func (h *Handler) readImage(r *http.Request) ([]byte, error) {
	contentType := r.Header.Get("Content-Type")

	// Multipart form data
	if len(contentType) > 19 && contentType[:19] == "multipart/form-data" {
		r.ParseMultipartForm(MaxUploadSize)
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("file required")
		}
		defer file.Close()
		return io.ReadAll(file)
	}

	// JSON with base64
	if contentType == "application/json" {
		var req RecognizeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, fmt.Errorf("invalid json")
		}
		if req.ImageBase64 == "" {
			return nil, fmt.Errorf("image_base64 required")
		}
		// Декодируем base64
		return base64.StdEncoding.DecodeString(req.ImageBase64)
	}

	// Raw binary
	if contentType == "application/octet-stream" || contentType == "image/jpeg" || contentType == "image/png" {
		return io.ReadAll(r.Body)
	}

	return nil, fmt.Errorf("unsupported content-type: %s", contentType)
}
