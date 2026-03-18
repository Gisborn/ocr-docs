package handler

// ErrorResponse represents an error response
// @Description Error response with code and message
type ErrorResponse struct {
	Error string `json:"error" example:"invalid api key"`
	Code  string `json:"code" example:"UNAUTHORIZED"`
}

// RecognizeRequest represents a recognize request
// @Description Request for passport OCR recognition
type RecognizeRequest struct {
	// File is the image file (JPEG, PNG, PDF)
	// in: formData
	// type: file
	File []byte `json:"file"`
	
	// DocumentType is the type of document to recognize
	// in: formData
	// example: passport_rf
	DocumentType string `json:"document_type"`
}

// RecognizeResponse represents a recognize response
// @Description OCR recognition result with passport fields
type RecognizeResponse struct {
	RequestID    string                 `json:"request_id" example:"req_abc123"`
	DocumentType string                 `json:"document_type" example:"passport_rf"`
	Fields       map[string]interface{} `json:"fields"`
	Confidences  map[string]float64     `json:"confidences"`
	Provider     string                 `json:"provider" example:"yandex"`
}

// BalanceResponse represents account balance
// @Description Account balance from billing service
type BalanceResponse struct {
	AccountID         int64   `json:"account_id" example:"123"`
	RealBalanceRub    float64 `json:"real_balance_rub" example:"15000.50"`
	PrepaidBalanceRub float64 `json:"prepaid_balance_rub" example:"5000.00"`
}

// HealthResponse represents health check response
// @Description Health check response
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}
