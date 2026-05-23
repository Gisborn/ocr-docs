package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"scan.passport.local/api/services/cabinet/internal/repository"
)

// SubscriptionService сервис для работы с подписками (прокси к Billing Service)
type SubscriptionService struct {
	repo         repository.Repository
	billingURL   string
	billingToken string
}

// NewSubscriptionService создает сервис подписок
func NewSubscriptionService(repo repository.Repository, billingURL, billingToken string) *SubscriptionService {
	return &SubscriptionService{
		repo:         repo,
		billingURL:   billingURL,
		billingToken: billingToken,
	}
}

// GetSubscriptionResponse ответ с информацией о подписке
type GetSubscriptionResponse struct {
	SubscriptionID    int32   `json:"subscription_id"`
	AccountID         int64   `json:"account_id"`
	TariffCode        string  `json:"tariff_code"`
	TariffName        string  `json:"tariff_name"`
	Status            string  `json:"status"`
	StartedAt         string  `json:"started_at"`
	ExpiresAt         string  `json:"expires_at"`
	AutoRenew         bool    `json:"auto_renew"`
	InitialPrepaidRub float64 `json:"initial_prepaid_rub"`
}

// CreateSubscriptionRequest запрос на создание подписки
type CreateSubscriptionRequest struct {
	TariffCode    string `json:"tariff_code"`
	PaymentMethod string `json:"payment_method"`
}

// GetSubscription получает активную подписку из Billing Service
func (s *SubscriptionService) GetSubscription(ctx context.Context, accountID int64) (*GetSubscriptionResponse, error) {
	if s.billingURL == "" {
		return nil, fmt.Errorf("billing service not configured")
	}

	url := fmt.Sprintf("%s/accounts/%d/subscriptions", s.billingURL, accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if s.billingToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.billingToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no active subscription")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("billing service error: %s", string(body))
	}

	var sub GetSubscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return nil, err
	}
	return &sub, nil
}

// CreateSubscription создает подписку через Billing Service
func (s *SubscriptionService) CreateSubscription(ctx context.Context, accountID int64, req *CreateSubscriptionRequest) (*GetSubscriptionResponse, error) {
	if s.billingURL == "" {
		return nil, fmt.Errorf("billing service not configured")
	}

	url := fmt.Sprintf("%s/accounts/%d/subscriptions", s.billingURL, accountID)
	payload, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.billingToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.billingToken)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusPaymentRequired {
			return nil, fmt.Errorf("insufficient balance")
		}
		return nil, fmt.Errorf("billing service error: %s", string(body))
	}

	var sub GetSubscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return nil, err
	}
	return &sub, nil
}
