package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"scan.passport.local/api/services/cabinet/internal/repository"
)

func TestSubscriptionService_GetSubscription_NotConfigured(t *testing.T) {
	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, "", "")

	_, err := svc.GetSubscription(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "billing service not configured") {
		t.Fatalf("expected not configured error, got %v", err)
	}
}

func TestSubscriptionService_GetSubscription_Success(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/accounts/1/subscriptions" {
			json.NewEncoder(w).Encode(GetSubscriptionResponse{
				SubscriptionID: 42,
				AccountID:      1,
				TariffCode:     "pro",
				TariffName:     "Pro",
				Status:         "active",
				StartedAt:      "2024-01-01T00:00:00Z",
				ExpiresAt:      "2025-01-01T00:00:00Z",
				AutoRenew:      true,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billingServer.Close()

	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, billingServer.URL, "token")

	resp, err := svc.GetSubscription(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SubscriptionID != 42 {
		t.Fatalf("expected subscription id 42, got %d", resp.SubscriptionID)
	}
	if resp.TariffCode != "pro" {
		t.Fatalf("expected tariff pro, got %s", resp.TariffCode)
	}
}

func TestSubscriptionService_GetSubscription_NotFound(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billingServer.Close()

	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, billingServer.URL, "token")

	_, err := svc.GetSubscription(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "no active subscription") {
		t.Fatalf("expected no active subscription error, got %v", err)
	}
}

func TestSubscriptionService_GetSubscription_BillingError(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer billingServer.Close()

	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, billingServer.URL, "token")

	_, err := svc.GetSubscription(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "billing service error") {
		t.Fatalf("expected billing service error, got %v", err)
	}
}

func TestSubscriptionService_GetSubscription_NetworkError(t *testing.T) {
	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, "http://localhost:1", "token")

	_, err := svc.GetSubscription(context.Background(), 1)
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestSubscriptionService_CreateSubscription_NotConfigured(t *testing.T) {
	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, "", "")

	_, err := svc.CreateSubscription(context.Background(), 1, &CreateSubscriptionRequest{TariffCode: "pro"})
	if err == nil || !strings.Contains(err.Error(), "billing service not configured") {
		t.Fatalf("expected not configured error, got %v", err)
	}
}

func TestSubscriptionService_CreateSubscription_Success(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/accounts/1/subscriptions" {
			var req CreateSubscriptionRequest
			json.NewDecoder(r.Body).Decode(&req)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(GetSubscriptionResponse{
				SubscriptionID: 99,
				AccountID:      1,
				TariffCode:     req.TariffCode,
				Status:         "active",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billingServer.Close()

	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, billingServer.URL, "token")

	resp, err := svc.CreateSubscription(context.Background(), 1, &CreateSubscriptionRequest{TariffCode: "pro", PaymentMethod: "balance"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SubscriptionID != 99 {
		t.Fatalf("expected subscription id 99, got %d", resp.SubscriptionID)
	}
	if resp.TariffCode != "pro" {
		t.Fatalf("expected tariff pro, got %s", resp.TariffCode)
	}
}

func TestSubscriptionService_CreateSubscription_InsufficientBalance(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte("insufficient balance"))
	}))
	defer billingServer.Close()

	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, billingServer.URL, "token")

	_, err := svc.CreateSubscription(context.Background(), 1, &CreateSubscriptionRequest{TariffCode: "pro"})
	if err == nil || !strings.Contains(err.Error(), "insufficient balance") {
		t.Fatalf("expected insufficient balance error, got %v", err)
	}
}

func TestSubscriptionService_CreateSubscription_BillingError(t *testing.T) {
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer billingServer.Close()

	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, billingServer.URL, "token")

	_, err := svc.CreateSubscription(context.Background(), 1, &CreateSubscriptionRequest{TariffCode: "pro"})
	if err == nil || !strings.Contains(err.Error(), "billing service error") {
		t.Fatalf("expected billing service error, got %v", err)
	}
}

func TestSubscriptionService_CreateSubscription_NetworkError(t *testing.T) {
	repo := NewMockRepository()
	svc := NewSubscriptionService(repo, "http://localhost:1", "token")

	_, err := svc.CreateSubscription(context.Background(), 1, &CreateSubscriptionRequest{TariffCode: "pro"})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestSubscriptionService_NewSubscriptionService(t *testing.T) {
	var repo repository.Repository = NewMockRepository()
	svc := NewSubscriptionService(repo, "http://billing", "token")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}
