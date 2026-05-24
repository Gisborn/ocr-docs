package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPaymentService_NewPaymentService(t *testing.T) {
	// We can't easily test with real db, but we can ensure constructor works
	// with nil db (it won't panic)
	svc := NewPaymentService(nil, "http://billing", "token")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestPaymentService_GetBalance_NotConfigured(t *testing.T) {
	svc := NewPaymentService(nil, "", "")

	_, err := svc.GetBalance(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "billing service not configured") {
		t.Fatalf("expected not configured error, got %v", err)
	}
}

func TestPaymentService_GetBalance_Success(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/accounts/1/balance" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"real_balance_rub":    500,
				"prepaid_balance_rub": 100,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billingServer.Close()

	svc := NewPaymentService(nil, billingServer.URL, "token")

	resp, err := svc.GetBalance(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp["real_balance_rub"] != float64(500) {
		t.Fatalf("expected real_balance_rub 500, got %v", resp["real_balance_rub"])
	}
}

func TestPaymentService_GetBalance_Error(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer billingServer.Close()

	svc := NewPaymentService(nil, billingServer.URL, "token")

	_, err := svc.GetBalance(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "billing service returned error") {
		t.Fatalf("expected billing service error, got %v", err)
	}
}

func TestPaymentService_GetBalance_NetworkError(t *testing.T) {
	svc := NewPaymentService(nil, "http://localhost:1", "token")

	_, err := svc.GetBalance(context.Background(), 1)
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestPaymentService_GetBillingEvents_NotConfigured(t *testing.T) {
	svc := NewPaymentService(nil, "", "")

	_, err := svc.GetBillingEvents(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "billing service not configured") {
		t.Fatalf("expected not configured error, got %v", err)
	}
}

func TestPaymentService_GetBillingEvents_Success(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/accounts/1/events" {
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 1, "type": "topup"},
				{"id": 2, "type": "commit"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billingServer.Close()

	svc := NewPaymentService(nil, billingServer.URL, "token")

	resp, err := svc.GetBillingEvents(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 events, got %d", len(resp))
	}
}

func TestPaymentService_GetBillingEvents_Error(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer billingServer.Close()

	svc := NewPaymentService(nil, billingServer.URL, "token")

	_, err := svc.GetBillingEvents(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "billing service returned error") {
		t.Fatalf("expected billing service error, got %v", err)
	}
}

func TestPaymentService_GetBillingEvents_NetworkError(t *testing.T) {
	svc := NewPaymentService(nil, "http://localhost:1", "token")

	_, err := svc.GetBillingEvents(context.Background(), 1)
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestPaymentService_GetBillingEvents_DecodeError(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/accounts/1/events" {
			w.Write([]byte("not json"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billingServer.Close()

	svc := NewPaymentService(nil, billingServer.URL, "token")

	_, err := svc.GetBillingEvents(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "failed to decode") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestPaymentService_GetBalance_DecodeError(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/accounts/1/balance" {
			w.Write([]byte("not json"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billingServer.Close()

	svc := NewPaymentService(nil, billingServer.URL, "token")

	_, err := svc.GetBalance(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "failed to decode") {
		t.Fatalf("expected decode error, got %v", err)
	}
}
