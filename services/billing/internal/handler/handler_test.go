package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"scan.passport.local/api/services/billing/internal/service"
	"scan.passport.local/api/services/billing/pkg/models"
)

func setupTestHandler() (*Handler, *service.MockRepository) {
	repo := service.NewMockRepository()
	repo.SeedTestData()

	billingSvc := service.NewBillingService(repo)
	subSvc := service.NewSubscriptionService(repo)
	paySvc := service.NewPaymentService(repo, nil, "")

	h := NewHandler(billingSvc, subSvc, paySvc, "")
	return h, repo
}

func TestHandler_Health(t *testing.T) {
	h, _ := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("Expected status ok, got %v", resp["status"])
	}
}

func TestHandler_CreateAccount(t *testing.T) {
	h, _ := setupTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/accounts", nil)
	rr := httptest.NewRecorder()

	h.CreateAccount(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d", rr.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["id"] == nil {
		t.Error("Expected id in response")
	}
}

func TestHandler_GetBalance(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.UpdateBalanceSnapshot(nil, &models.BalanceSnapshot{
		AccountID:         acc.ID,
		RealBalanceRub:    1500,
		PrepaidBalanceRub: 300,
	})

	req := httptest.NewRequest(http.MethodGet, "/accounts/1/balance", nil)
	rr := httptest.NewRecorder()

	h.GetBalance(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["real_balance_rub"] == nil {
		t.Error("Expected real_balance_rub in response")
	}
}

func TestHandler_GetBalanceInvalidID(t *testing.T) {
	h, _ := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/accounts/abc/balance", nil)
	rr := httptest.NewRecorder()

	h.GetBalance(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}

func TestHandler_ReserveAndCommit(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetBalance(acc.ID, 1000, 0)

	// Reserve
	body, _ := json.Marshal(service.ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_test_001",
	})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/reserve", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.Reserve(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Reserve expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var reserveResp service.ReserveResponse
	json.NewDecoder(rr.Body).Decode(&reserveResp)
	if !reserveResp.Reserved {
		t.Error("Expected reserved=true")
	}

	// Commit
	req = httptest.NewRequest(http.MethodPost, "/transactions/req_test_001/commit", nil)
	rr = httptest.NewRecorder()

	h.Commit(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Commit expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var commitResp SuccessResponse
	json.NewDecoder(rr.Body).Decode(&commitResp)
	if commitResp.Status != "committed" {
		t.Errorf("Expected status committed, got %s", commitResp.Status)
	}
}

func TestHandler_ReserveInsufficientBalance(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetBalance(acc.ID, 0, 0)

	body, _ := json.Marshal(service.ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_insufficient",
	})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/reserve", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.Reserve(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected 402, got %d", rr.Code)
	}
}

func TestHandler_Rollback(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetBalance(acc.ID, 1000, 0)

	// Reserve first
	body, _ := json.Marshal(service.ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_rollback",
	})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/reserve", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	h.Reserve(rr, req)

	// Rollback
	req = httptest.NewRequest(http.MethodPost, "/transactions/req_rollback/rollback", nil)
	rr = httptest.NewRecorder()

	h.Rollback(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp SuccessResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Status != "rolled back" {
		t.Errorf("Expected 'rolled back', got %s", resp.Status)
	}
}

func TestHandler_TopupBalance(t *testing.T) {
	h, repo := setupTestHandler()

	repo.CreateAccount(nil)

	body, _ := json.Marshal(map[string]float64{"amount_rub": 500})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/topup", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.TopupBalance(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "success" {
		t.Errorf("Expected success, got %v", resp["status"])
	}
}

func TestHandler_TopupBalanceInvalidAmount(t *testing.T) {
	h, repo := setupTestHandler()
	acc, _ := repo.CreateAccount(nil)
	_ = acc

	body, _ := json.Marshal(map[string]float64{"amount_rub": -100})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/topup", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.TopupBalance(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}

func TestHandler_GetBillingEvents(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetBalance(acc.ID, 1000, 0)

	// Create a billing event via topup
	subSvc := service.NewSubscriptionService(repo)
	subSvc.CreateTopupEvent(nil, acc.ID, 500)

	req := httptest.NewRequest(http.MethodGet, "/accounts/1/events", nil)
	rr := httptest.NewRecorder()

	h.GetBillingEvents(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp []map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Errorf("Expected 1 event, got %d", len(resp))
	}
}

func TestHandler_CreateSubscription(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetBalance(acc.ID, 2000, 0)

	body, _ := json.Marshal(service.CreateSubscriptionRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/subscriptions", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.CreateSubscription(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp service.CreateSubscriptionResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Status != "active" {
		t.Errorf("Expected active, got %s", resp.Status)
	}
}

func TestHandler_CreateSubscriptionInsufficientBalance(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetBalance(acc.ID, 0, 0)

	body, _ := json.Marshal(service.CreateSubscriptionRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/subscriptions", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.CreateSubscription(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected 402, got %d", rr.Code)
	}
}

func TestHandler_GetAccountSubscription(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetBalance(acc.ID, 2000, 0)

	// Create subscription
	body, _ := json.Marshal(service.CreateSubscriptionRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/subscriptions", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	h.CreateSubscription(rr, req)

	// Get subscription
	req = httptest.NewRequest(http.MethodGet, "/accounts/1/subscriptions", nil)
	rr = httptest.NewRecorder()

	h.GetAccountSubscription(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp service.GetActiveSubscriptionResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.TariffCode != "pro" {
		t.Errorf("Expected pro, got %s", resp.TariffCode)
	}
}

func TestHandler_GetAccountSubscriptionNotFound(t *testing.T) {
	h, repo := setupTestHandler()
	repo.CreateAccount(nil)

	req := httptest.NewRequest(http.MethodGet, "/accounts/1/subscriptions", nil)
	rr := httptest.NewRecorder()

	h.GetAccountSubscription(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rr.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h, _ := setupTestHandler()

	endpoints := []struct {
		method string
		path   string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{http.MethodPost, "/health", h.Health},
		{http.MethodGet, "/accounts", h.CreateAccount},
		{http.MethodPost, "/accounts/1/balance", h.GetBalance},
		{http.MethodGet, "/accounts/1/reserve", h.Reserve},
		{http.MethodGet, "/transactions/1/commit", h.Commit},
		{http.MethodGet, "/transactions/1/rollback", h.Rollback},
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		rr := httptest.NewRecorder()
		ep.handler(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: expected 405, got %d", ep.method, ep.path, rr.Code)
		}
	}
}

func TestHandler_CreatePayment(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetAccountStatus(acc.ID, "active")

	body, _ := json.Marshal(service.CreatePaymentRequest{
		AmountRub:   1000,
		Description: "Test payment",
	})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/payments", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.CreatePayment(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp service.CreatePaymentResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Status != "pending" {
		t.Errorf("Expected pending, got %s", resp.Status)
	}
	if !strings.Contains(resp.PaymentURL, "yookassa.ru") {
		t.Errorf("Expected yookassa URL, got %s", resp.PaymentURL)
	}
}

func TestHandler_GetPayment(t *testing.T) {
	h, repo := setupTestHandler()

	acc, _ := repo.CreateAccount(nil)
	repo.SetAccountStatus(acc.ID, "active")

	// Create payment
	body, _ := json.Marshal(service.CreatePaymentRequest{AmountRub: 500})
	req := httptest.NewRequest(http.MethodPost, "/accounts/1/payments", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	h.CreatePayment(rr, req)

	var createResp service.CreatePaymentResponse
	json.NewDecoder(rr.Body).Decode(&createResp)

	// Get payment
	req = httptest.NewRequest(http.MethodGet, "/payments/1", nil)
	rr = httptest.NewRecorder()

	h.GetPayment(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["id"] == nil {
		t.Error("Expected id in payment response")
	}
}

func TestHandler_GetPaymentNotFound(t *testing.T) {
	h, _ := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/payments/999", nil)
	rr := httptest.NewRecorder()

	h.GetPayment(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rr.Code)
	}
}
