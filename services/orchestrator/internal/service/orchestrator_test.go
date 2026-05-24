package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scan.passport.local/api/pkg/ocr"
)

// mockOCRProvider мок OCR провайдера
type mockOCRProvider struct {
	name       string
	result     *ocr.Result
	err        error
	callCount  int
}

func (m *mockOCRProvider) Recognize(ctx context.Context, image []byte) (*ocr.Result, error) {
	m.callCount++
	return m.result, m.err
}

func (m *mockOCRProvider) Name() string {
	return m.name
}

func makeOCRResult(confidence float64) *ocr.Result {
	return &ocr.Result{
		Fields: map[string]ocr.Field{
			"last_name":       {Value: "Иванов", Confidence: confidence},
			"first_name":      {Value: "Иван", Confidence: confidence},
			"middle_name":     {Value: "Иванович", Confidence: confidence},
			"birth_date":      {Value: "01.01.1990", Confidence: confidence},
			"series":          {Value: "4515", Confidence: confidence},
			"number":          {Value: "123456", Confidence: confidence},
			"issue_date":      {Value: "15.05.2015", Confidence: confidence},
			"issued_by":       {Value: "Отделом УФМС", Confidence: confidence},
			"division_code":   {Value: "770-064", Confidence: confidence},
		},
	}
}

func setupBillingServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/accounts/1/reserve":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(ReserveResponse{
				Reserved:      true,
				TransactionID: "txn_123",
				ChargeType:    "pay_as_you_go",
				AmountRub:     7.0,
			})
		case r.Method == "POST" && r.URL.Path == "/transactions/txn_123/commit":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"committed"}`)
		case r.Method == "POST" && r.URL.Path == "/transactions/txn_123/rollback":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"rolled back"}`)
		case r.Method == "POST" && r.URL.Path == "/accounts/99/reserve":
			w.WriteHeader(http.StatusPaymentRequired)
			fmt.Fprint(w, `{"error":"insufficient balance"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// ── Orchestrator Tests ──

func TestOrchestrator_ProcessRequest_PrimarySuccess(t *testing.T) {
	primary := &mockOCRProvider{
		name:   "primary",
		result: makeOCRResult(0.95),
	}
	fallback := &mockOCRProvider{name: "fallback"}

	orch := NewOrchestrator(primary, fallback)
	result, err := orch.ProcessRequest(context.Background(), []byte("image"))

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result.Fields.LastName != "Иванов" {
		t.Errorf("Expected Иванов, got %s", result.Fields.LastName)
	}
	if primary.callCount != 1 {
		t.Errorf("Expected primary called once, got %d", primary.callCount)
	}
	if fallback.callCount != 0 {
		t.Errorf("Expected fallback not called, got %d", fallback.callCount)
	}
}

func TestOrchestrator_ProcessRequest_FallbackSuccess(t *testing.T) {
	primary := &mockOCRProvider{
		name: "primary",
		err:  &ocr.ProviderError{Provider: "primary", Type: ocr.ErrorTypeNetwork, Message: "timeout"},
	}
	fallback := &mockOCRProvider{
		name:   "fallback",
		result: makeOCRResult(0.90),
	}

	orch := NewOrchestrator(primary, fallback)
	result, err := orch.ProcessRequest(context.Background(), []byte("image"))

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result.Fields.LastName != "Иванов" {
		t.Errorf("Expected Иванов, got %s", result.Fields.LastName)
	}
	if primary.callCount != 1 {
		t.Errorf("Expected primary called once, got %d", primary.callCount)
	}
	if fallback.callCount != 1 {
		t.Errorf("Expected fallback called once, got %d", fallback.callCount)
	}
}

func TestOrchestrator_ProcessRequest_BothFail(t *testing.T) {
	primary := &mockOCRProvider{
		name: "primary",
		err:  &ocr.ProviderError{Provider: "primary", Type: ocr.ErrorTypeNetwork, Message: "timeout"},
	}
	fallback := &mockOCRProvider{
		name: "fallback",
		err:  &ocr.ProviderError{Provider: "fallback", Type: ocr.ErrorTypeAPI, Message: "500"},
	}

	orch := NewOrchestrator(primary, fallback)
	_, err := orch.ProcessRequest(context.Background(), []byte("image"))

	if err == nil {
		t.Fatal("Expected error when both providers fail")
	}
}

func TestOrchestrator_ProcessRequest_NonRetryableError(t *testing.T) {
	primary := &mockOCRProvider{
		name: "primary",
		err:  &ocr.ProviderError{Provider: "primary", Type: ocr.ErrorTypeAuth, Message: "unauthorized"},
	}
	fallback := &mockOCRProvider{name: "fallback"}

	orch := NewOrchestrator(primary, fallback)
	_, err := orch.ProcessRequest(context.Background(), []byte("image"))

	if err == nil {
		t.Fatal("Expected error for non-retryable auth error")
	}
	if fallback.callCount != 0 {
		t.Error("Expected fallback not called for non-retryable error")
	}
}

// ── FullOrchestrator Tests ──

func TestFullOrchestrator_Process_Success(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	billingClient := NewBillingClient(billing.URL, "test-token")
	primary := &mockOCRProvider{name: "primary", result: makeOCRResult(0.95)}
	fallback := &mockOCRProvider{name: "fallback"}

	orch := NewFullOrchestrator(billingClient, primary, fallback, 0.80)
	result, err := orch.Process(context.Background(), 1, "req_001", []byte("image"))

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result.Data.Fields.LastName != "Иванов" {
		t.Errorf("Expected Иванов, got %s", result.Data.Fields.LastName)
	}
	if result.Provider != "primary" {
		t.Errorf("Expected provider=primary, got %s", result.Provider)
	}
}

func TestFullOrchestrator_Process_ReserveFails(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	billingClient := NewBillingClient(billing.URL, "test-token")
	primary := &mockOCRProvider{name: "primary", result: makeOCRResult(0.95)}

	orch := NewFullOrchestrator(billingClient, primary, nil, 0.80)
	_, err := orch.Process(context.Background(), 99, "req_002", []byte("image"))

	if err == nil {
		t.Fatal("Expected error for reserve failure")
	}
	if primary.callCount != 0 {
		t.Error("Expected OCR not called when reserve fails")
	}
}

func TestFullOrchestrator_Process_OCRFails_Rollback(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	billingClient := NewBillingClient(billing.URL, "test-token")
	primary := &mockOCRProvider{
		name: "primary",
		err:  &ocr.ProviderError{Provider: "primary", Type: ocr.ErrorTypeNetwork, Message: "timeout"},
	}
	fallback := &mockOCRProvider{
		name:   "fallback",
		err:    &ocr.ProviderError{Provider: "fallback", Type: ocr.ErrorTypeAPI, Message: "500"},
	}

	orch := NewFullOrchestrator(billingClient, primary, fallback, 0.80)
	_, err := orch.Process(context.Background(), 1, "req_003", []byte("image"))

	if err == nil {
		t.Fatal("Expected error when OCR fails")
	}
}

func TestFullOrchestrator_Process_LowConfidence_Rollback(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	billingClient := NewBillingClient(billing.URL, "test-token")
	primary := &mockOCRProvider{name: "primary", result: makeOCRResult(0.50)}
	fallback := &mockOCRProvider{name: "fallback"}

	orch := NewFullOrchestrator(billingClient, primary, fallback, 0.80)
	_, err := orch.Process(context.Background(), 1, "req_004", []byte("image"))

	if err == nil {
		t.Fatal("Expected error for low confidence")
	}
	if primary.callCount < 1 {
		t.Error("Expected primary OCR called")
	}
}

func TestFullOrchestrator_Process_FallbackSuccess(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	billingClient := NewBillingClient(billing.URL, "test-token")
	primary := &mockOCRProvider{
		name: "primary",
		err:  &ocr.ProviderError{Provider: "primary", Type: ocr.ErrorTypeNetwork, Message: "timeout"},
	}
	fallback := &mockOCRProvider{name: "fallback", result: makeOCRResult(0.95)}

	orch := NewFullOrchestrator(billingClient, primary, fallback, 0.80)
	result, err := orch.Process(context.Background(), 1, "req_005", []byte("image"))

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if result.Provider != "fallback" {
		t.Errorf("Expected provider=fallback, got %s", result.Provider)
	}
}

func TestFullOrchestrator_Stats(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	billingClient := NewBillingClient(billing.URL, "test-token")
	orch := NewFullOrchestrator(billingClient, nil, nil, 0.80)

	stats := orch.Stats()
	if stats["primary_circuit_breaker"] == nil {
		t.Error("Expected primary circuit breaker stats")
	}
	if stats["fallback_circuit_breaker"] == nil {
		t.Error("Expected fallback circuit breaker stats")
	}
}

// ── CircuitBreaker Tests ──

func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)
	if !cb.Allow() {
		t.Error("Expected Allow=true in closed state")
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state=closed, got %v", cb.State())
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("Expected state=open after 3 failures, got %v", cb.State())
	}
	if cb.Allow() {
		t.Error("Expected Allow=false in open state")
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 50*time.Millisecond)
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatal("Expected state=open")
	}

	time.Sleep(100 * time.Millisecond)

	if !cb.Allow() {
		t.Error("Expected Allow=true after timeout (half-open)")
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("Expected state=half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_ClosesAfterSuccesses(t *testing.T) {
	cb := NewCircuitBreaker("test", 5, time.Minute)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatal("Expected state=open")
	}

	// Simulate half-open by waiting
	// Directly set to half-open for test
	cb.mutex.Lock()
	cb.state = StateHalfOpen
	cb.mutex.Unlock()

	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Errorf("Expected state=closed after 2 successes in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker("test", 5, time.Minute)
	cb.mutex.Lock()
	cb.state = StateHalfOpen
	cb.mutex.Unlock()

	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("Expected state=open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := NewCircuitBreaker("test-cb", 5, time.Minute)
	cb.RecordFailure()
	cb.RecordFailure()

	stats := cb.Stats()
	if stats["name"] != "test-cb" {
		t.Errorf("Expected name=test-cb, got %v", stats["name"])
	}
	if stats["failure_count"] != 2 {
		t.Errorf("Expected failure_count=2, got %v", stats["failure_count"])
	}
}

func TestCircuitBreaker_DefaultValues(t *testing.T) {
	cb := NewCircuitBreaker("", 0, 0)
	if cb.failureThreshold != 5 {
		t.Errorf("Expected default failureThreshold=5, got %d", cb.failureThreshold)
	}
}

func TestState_String(t *testing.T) {
	if StateClosed.String() != "closed" {
		t.Errorf("Expected closed, got %s", StateClosed.String())
	}
	if StateOpen.String() != "open" {
		t.Errorf("Expected open, got %s", StateOpen.String())
	}
	if StateHalfOpen.String() != "half-open" {
		t.Errorf("Expected half-open, got %s", StateHalfOpen.String())
	}
	if State(999).String() != "unknown" {
		t.Errorf("Expected unknown, got %s", State(999).String())
	}
}

// ── BillingClient Tests ──

func TestBillingClient_Reserve_Success(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	client := NewBillingClient(billing.URL, "test-token")
	resp, err := client.Reserve(context.Background(), 1, &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_001",
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !resp.Reserved {
		t.Error("Expected reserved=true")
	}
}

func TestBillingClient_Reserve_InsufficientBalance(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	client := NewBillingClient(billing.URL, "test-token")
	_, err := client.Reserve(context.Background(), 99, &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_002",
	})

	if err != ErrInsufficientBalance {
		t.Fatalf("Expected ErrInsufficientBalance, got %v", err)
	}
}

func TestBillingClient_Commit(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	client := NewBillingClient(billing.URL, "test-token")
	err := client.Commit(context.Background(), "txn_123")

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestBillingClient_Rollback(t *testing.T) {
	billing := setupBillingServer()
	defer billing.Close()

	client := NewBillingClient(billing.URL, "test-token")
	err := client.Rollback(context.Background(), "txn_123", "test reason")

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestBillingClient_Reserve_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	}))
	defer server.Close()

	client := NewBillingClient(server.URL, "test-token")
	_, err := client.Reserve(context.Background(), 1, &ReserveRequest{})

	if err == nil {
		t.Fatal("Expected error for 500 response")
	}
}

func TestBillingClient_Reserve_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewBillingClient(server.URL, "test-token")
	_, err := client.Reserve(context.Background(), 1, &ReserveRequest{})

	if err != ErrUnauthorized {
		t.Fatalf("Expected ErrUnauthorized, got %v", err)
	}
}

func TestBillingClient_Reserve_ServiceUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewBillingClient(server.URL, "test-token")
	_, err := client.Reserve(context.Background(), 1, &ReserveRequest{})

	if err != ErrBillingUnavailable {
		t.Fatalf("Expected ErrBillingUnavailable, got %v", err)
	}
}

func TestBillingClient_Commit_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewBillingClient(server.URL, "test-token")
	err := client.Commit(context.Background(), "txn_999")

	if err == nil {
		t.Fatal("Expected error for 500 response")
	}
}

// ── PassportNormalizer Test ──

func TestPassportNormalizer_Normalize(t *testing.T) {
	n := &PassportNormalizer{}
	fields := map[string]string{
		"last_name":     "Иванов",
		"first_name":    "Иван",
		"middle_name":   "Иванович",
		"birth_date":    "01.01.1990",
		"series":        "4515",
		"number":        "123456",
		"issue_date":    "15.05.2015",
		"issued_by":     "Отделом УФМС",
		"division_code": "770-064",
	}
	confidences := map[string]float64{
		"last_name": 0.95, "first_name": 0.95, "middle_name": 0.95,
	}

	result := n.Normalize(fields, confidences)
	if result.Fields.LastName != "Иванов" {
		t.Errorf("Expected Иванов, got %s", result.Fields.LastName)
	}
}

// ── OCR Result Confidence Test ──

func TestOCRResult_Confidence(t *testing.T) {
	r := &ocr.Result{
		Fields: map[string]ocr.Field{
			"a": {Value: "1", Confidence: 0.9},
			"b": {Value: "2", Confidence: 0.5},
			"c": {Value: "3", Confidence: 0.8},
		},
	}
	if r.Confidence() != 0.5 {
		t.Errorf("Expected min confidence 0.5, got %.2f", r.Confidence())
	}

	empty := &ocr.Result{Fields: map[string]ocr.Field{}}
	if empty.Confidence() != 0 {
		t.Errorf("Expected 0 for empty fields, got %.2f", empty.Confidence())
	}
}
