package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/api-scan/api-scan/services/orchestrator/internal/service"
)

// Handler HTTP обработчики
type Handler struct {
	orchestrator *service.Orchestrator
}

// New создает новый handler
func New(orch *service.Orchestrator) *Handler {
	return &Handler{
		orchestrator: orch,
	}
}

// RecognitionRequest запрос на распознавание
type RecognitionRequest struct {
	DocumentType string `json:"document_type"` // passport_rf (default)
}

// RecognitionResponse ответ на распознавание
type RecognitionResponse struct {
	RequestID    string                 `json:"request_id"`
	DocumentType string                 `json:"document_type"`
	Data         map[string]interface{} `json:"data"`
	Confidences  map[string]float64     `json:"confidences"`
	ProviderUsed string                 `json:"provider_used"`
}

// ErrorResponse ответ с ошибкой
type ErrorResponse struct {
	RequestID string `json:"request_id"`
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
}

// RecognizeHandler обрабатывает POST /v1/recognize
func (h *Handler) RecognizeHandler(w http.ResponseWriter, r *http.Request) {
	requestID := generateRequestID()
	ctx := r.Context()
	
	// Проверяем метод
	if r.Method != http.MethodPost {
		h.writeError(w, r, requestID, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}
	
	// Парсим multipart form
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB
		h.writeError(w, r, requestID, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse multipart form")
		return
	}
	
	// Получаем файл
	file, header, err := r.FormFile("file")
	if err != nil {
		h.writeError(w, r, requestID, http.StatusBadRequest, "FILE_REQUIRED", "Field 'file' is required")
		return
	}
	defer file.Close()
	
	// Проверяем размер файла
	if header.Size > 10<<20 { // 10 MB
		h.writeError(w, r, requestID, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "File size exceeds 10 MB limit")
		return
	}
	
	// Проверяем content-type
	contentType := header.Header.Get("Content-Type")
	if !isValidContentType(contentType) {
		h.writeError(w, r, requestID, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", 
			"Supported formats: JPEG, PNG, PDF")
		return
	}
	
	// Читаем файл
	imageBytes, err := io.ReadAll(file)
	if err != nil {
		slog.Error("failed to read file", "error", err)
		h.writeError(w, r, requestID, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to read file")
		return
	}
	
	// Получаем document_type (опционально, default: passport_rf)
	documentType := r.FormValue("document_type")
	if documentType == "" {
		documentType = "passport_rf"
	}
	
	// В MVP поддерживаем только passport_rf
	if documentType != "passport_rf" {
		h.writeError(w, r, requestID, http.StatusBadRequest, "UNSUPPORTED_DOCUMENT_TYPE", 
			fmt.Sprintf("Document type '%s' is not supported in MVP. Only 'passport_rf' is available.", documentType))
		return
	}
	
	// Выполняем распознавание
	result, err := h.orchestrator.Recognize(ctx, imageBytes, documentType)
	if err != nil {
		slog.Error("recognition failed", "error", err, "request_id", requestID)
		
		// Определяем тип ошибки
		var lowConfErr *service.LowConfidenceError
		if errors.As(err, &lowConfErr) {
			h.writeError(w, r, requestID, http.StatusUnprocessableEntity, "LOW_CONFIDENCE", 
				"Could not recognize document with sufficient confidence. Please provide a clearer image.")
			return
		}
		
		h.writeError(w, r, requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", 
			"Recognition service temporarily unavailable. Please try again later.")
		return
	}
	
	// Формируем ответ
	response := RecognitionResponse{
		RequestID:    requestID,
		DocumentType: result.DocumentType,
		Data: map[string]interface{}{
			"last_name":             result.PassportData.LastName,
			"first_name":            result.PassportData.FirstName,
			"middle_name":           result.PassportData.MiddleName,
			"birth_date":            result.PassportData.BirthDate,
			"series":                result.PassportData.Series,
			"number":                result.PassportData.Number,
			"issue_date":            result.PassportData.IssueDate,
			"issued_by":             result.PassportData.IssuedBy,
			"division_code":         result.PassportData.DivisionCode,
			"registration_address":  result.PassportData.RegistrationAddress,
		},
		Confidences: map[string]float64{
			"last_name":             result.Confidences.LastName,
			"first_name":            result.Confidences.FirstName,
			"middle_name":           result.Confidences.MiddleName,
			"birth_date":            result.Confidences.BirthDate,
			"series":                result.Confidences.Series,
			"number":                result.Confidences.Number,
			"issue_date":            result.Confidences.IssueDate,
			"issued_by":             result.Confidences.IssuedBy,
			"division_code":         result.Confidences.DivisionCode,
			"registration_address":  result.Confidences.RegistrationAddress,
		},
		ProviderUsed: result.ProviderUsed,
	}
	
	h.writeJSON(w, r, http.StatusOK, response)
}

// HealthHandler проверка здоровья
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// StatsHandler статистика сервиса
func (h *Handler) StatsHandler(w http.ResponseWriter, r *http.Request) {
	stats := h.orchestrator.GetStats()
	h.writeJSON(w, r, http.StatusOK, stats)
}

func (h *Handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, requestID string, status int, code, message string) {
	slog.Warn("request error",
		"request_id", requestID,
		"status", status,
		"code", code,
		"message", message,
	)
	
	response := ErrorResponse{
		RequestID: requestID,
		Error:     message,
		Code:      code,
	}
	
	h.writeJSON(w, r, status, response)
}

func isValidContentType(contentType string) bool {
	validTypes := []string{
		"image/jpeg",
		"image/jpg",
		"image/png",
		"application/pdf",
	}
	
	for _, t := range validTypes {
		if contentType == t {
			return true
		}
	}
	
	return false
}

func generateRequestID() string {
	// TODO: использовать ULID или UUID
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
