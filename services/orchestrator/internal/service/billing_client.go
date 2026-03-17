package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BillingClient клиент для взаимодействия с Billing Service
type BillingClient struct {
	baseURL    string
	serviceToken string
	httpClient *http.Client
}

// NewBillingClient создает новый клиент Billing Service
func NewBillingClient(baseURL, serviceToken string) *BillingClient {
	return &BillingClient{
		baseURL:      baseURL,
		serviceToken: serviceToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ReserveRequest запрос на резервирование средств
type ReserveRequest struct {
	ServiceID string `json:"service_id"`
	RequestID string `json:"request_id"`
}

// ReserveResponse ответ на резервирование
type ReserveResponse struct {
	Reserved      bool    `json:"reserved"`
	TransactionID string  `json:"transaction_id"`
	ChargeType    string  `json:"charge_type"`
	AmountRub     float64 `json:"amount_rub"`
}

// Reserve выполняет резервирование средств перед OCR
func (c *BillingClient) Reserve(ctx context.Context, accountID int64, req *ReserveRequest) (*ReserveResponse, error) {
	url := fmt.Sprintf("%s/accounts/%d/reserve", c.baseURL, accountID)
	
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.serviceToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	return c.handleResponse(resp)
}

// Commit фиксирует транзакцию после успешного OCR
func (c *BillingClient) Commit(ctx context.Context, transactionID string) error {
	url := fmt.Sprintf("%s/transactions/%s/commit", c.baseURL, transactionID)
	
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.serviceToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("commit failed: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Rollback откатывает транзакцию при ошибке OCR
func (c *BillingClient) Rollback(ctx context.Context, transactionID string, reason string) error {
	url := fmt.Sprintf("%s/transactions/%s/rollback", c.baseURL, transactionID)
	
	body, _ := json.Marshal(map[string]string{"reason": reason})
	
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.serviceToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rollback failed: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// handleResponse обрабатывает HTTP ответ
func (c *BillingClient) handleResponse(resp *http.Response) (*ReserveResponse, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var result ReserveResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return &result, nil
	case http.StatusPaymentRequired:
		return nil, ErrInsufficientBalance
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusServiceUnavailable:
		return nil, ErrBillingUnavailable
	default:
		return nil, fmt.Errorf("unexpected status: %d, body: %s", resp.StatusCode, string(body))
	}
}

// Ошибки Billing Client
var (
	ErrInsufficientBalance = fmt.Errorf("insufficient balance")
	ErrUnauthorized        = fmt.Errorf("unauthorized")
	ErrBillingUnavailable  = fmt.Errorf("billing service unavailable")
)
