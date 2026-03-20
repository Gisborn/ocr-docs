package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PaymentService сервис для работы с платежами
type PaymentService struct {
	db            *pgxpool.Pool
	billingURL    string
	billingToken  string
}

// NewPaymentService создает сервис платежей
func NewPaymentService(db *pgxpool.Pool, billingURL, billingToken string) *PaymentService {
	return &PaymentService{
		db:           db,
		billingURL:   billingURL,
		billingToken: billingToken,
	}
}

// MockPaymentRequest запрос на создание мок-платежа
type MockPaymentRequest struct {
	AmountRub int64 `json:"amount_rub"`
}

// MockPaymentResponse ответ с созданным платежом
type MockPaymentResponse struct {
	PaymentID   string `json:"payment_id"`
	AmountRub   int64  `json:"amount_rub"`
	Status      string `json:"status"`
	PaymentURL  string `json:"payment_url"`
	Description string `json:"description"`
}

// CreateMockPayment создает мок-платеж в БД
func (s *PaymentService) CreateMockPayment(ctx context.Context, orgID int64, accountID int64, req *MockPaymentRequest) (*MockPaymentResponse, error) {
	log.Printf("[MockPayment] Creating for org=%d, account=%d, amount=%d", orgID, accountID, req.AmountRub)

	// Генерируем ID платежа
	paymentID := fmt.Sprintf("mock_%d_%d", time.Now().Unix(), orgID)

	// Создаем запись о платеже в БД
	// В реальности здесь был бы запрос к Billing Service
	_, err := s.db.Exec(ctx, `
		INSERT INTO mock_payments (payment_id, org_id, account_id, amount_rub, status, created_at)
		VALUES ($1, $2, $3, $4, 'pending', NOW())
	`, paymentID, orgID, accountID, req.AmountRub)

	if err != nil {
		log.Printf("[MockPayment] Failed to create payment: %v", err)
		return nil, fmt.Errorf("failed to create payment: %w", err)
	}

	log.Printf("[MockPayment] Created: %s", paymentID)

	return &MockPaymentResponse{
		PaymentID:   paymentID,
		AmountRub:   req.AmountRub,
		Status:      "pending",
		PaymentURL:  "/mock/payment/" + paymentID,
		Description: fmt.Sprintf("Пополнение баланса на %d₽", req.AmountRub),
	}, nil
}

// ConfirmMockPayment подтверждает мок-платеж (симуляция webhook от ЮКассы)
func (s *PaymentService) ConfirmMockPayment(ctx context.Context, paymentID string) error {
	log.Printf("[MockPayment] Confirming: %s", paymentID)

	// Получаем информацию о платеже
	var orgID, accountID int64
	var amountRub int64
	err := s.db.QueryRow(ctx, `
		SELECT org_id, account_id, amount_rub FROM mock_payments 
		WHERE payment_id = $1 AND status = 'pending'
	`, paymentID).Scan(&orgID, &accountID, &amountRub)

	if err != nil {
		log.Printf("[MockPayment] Payment not found or already processed: %v", err)
		return fmt.Errorf("payment not found or already processed")
	}

	// Вызываем Billing Service для пополнения баланса
	if s.billingURL != "" {
		topupURL := fmt.Sprintf("%s/accounts/%d/topup", s.billingURL, accountID)
		payload := fmt.Sprintf(`{"amount_rub": %d}`, amountRub)
		
		req, err := http.NewRequestWithContext(ctx, "POST", topupURL, strings.NewReader(payload))
		if err != nil {
			log.Printf("[MockPayment] Failed to create topup request: %v", err)
			return fmt.Errorf("failed to create topup request")
		}
		req.Header.Set("Content-Type", "application/json")
		if s.billingToken != "" {
			req.Header.Set("X-Service-Token", s.billingToken)
		}
		
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[MockPayment] Failed to call billing: %v", err)
			return fmt.Errorf("billing service unavailable")
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			log.Printf("[MockPayment] Billing returned error: %d", resp.StatusCode)
			return fmt.Errorf("billing service error")
		}
		
		log.Printf("[MockPayment] Billing topup successful for account %d, amount %d", accountID, amountRub)
	}

	// Обновляем статус платежа
	_, err = s.db.Exec(ctx, `
		UPDATE mock_payments SET status = 'completed', completed_at = NOW()
		WHERE payment_id = $1
	`, paymentID)

	if err != nil {
		log.Printf("[MockPayment] Failed to update payment: %v", err)
		return fmt.Errorf("failed to update payment")
	}

	log.Printf("[MockPayment] Confirmed: %s, amount: %d, account: %d", paymentID, amountRub, accountID)

	return nil
}

// GetMockPayment получает статус мок-платежа
func (s *PaymentService) GetMockPayment(ctx context.Context, paymentID string) (map[string]interface{}, error) {
	var status string
	var amountRub int64
	var createdAt time.Time
	var completedAt *time.Time

	err := s.db.QueryRow(ctx, `
		SELECT status, amount_rub, created_at, completed_at FROM mock_payments 
		WHERE payment_id = $1
	`, paymentID).Scan(&status, &amountRub, &createdAt, &completedAt)

	if err != nil {
		return nil, fmt.Errorf("payment not found")
	}

	result := map[string]interface{}{
		"payment_id": paymentID,
		"status":     status,
		"amount_rub": amountRub,
		"created_at": createdAt,
	}

	if completedAt != nil {
		result["completed_at"] = *completedAt
	}

	return result, nil
}


// GetBalance получает баланс из Billing Service
func (s *PaymentService) GetBalance(ctx context.Context, accountID int64) (map[string]interface{}, error) {
	if s.billingURL == "" {
		return nil, fmt.Errorf("billing service not configured")
	}

	balanceURL := fmt.Sprintf("%s/accounts/%d/balance", s.billingURL, accountID)
	
	req, err := http.NewRequestWithContext(ctx, "GET", balanceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create balance request: %w", err)
	}
	
	if s.billingToken != "" {
		req.Header.Set("X-Service-Token", s.billingToken)
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("billing service unavailable: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("billing service returned error: %d", resp.StatusCode)
	}
	
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode balance response: %w", err)
	}
	
	return result, nil
}
