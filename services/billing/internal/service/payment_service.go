package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"scan.passport.local/api/services/billing/internal/repository"
	"scan.passport.local/api/services/billing/pkg/models"
)

// PaymentService сервис управления платежами
type PaymentService struct {
	repo             repository.Repository
	yookassaClient   *YooKassaClient
	webhookBaseURL   string
}

// YooKassaClient клиент для работы с API ЮКассы
type YooKassaClient struct {
	shopID     string
	secretKey  string
	httpClient *http.Client
	baseURL    string
}

// NewYooKassaClient создает клиента ЮКассы
func NewYooKassaClient(shopID, secretKey string) *YooKassaClient {
	return &YooKassaClient{
		shopID:     shopID,
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://api.yookassa.ru/v3",
	}
}

// NewPaymentService создает сервис платежей
func NewPaymentService(repo repository.Repository, yookassa *YooKassaClient, webhookBaseURL string) *PaymentService {
	return &PaymentService{
		repo:           repo,
		yookassaClient: yookassa,
		webhookBaseURL: webhookBaseURL,
	}
}

// CreatePaymentRequest запрос на создание платежа
type CreatePaymentRequest struct {
	AmountRub   float64 `json:"amount_rub"`
	Description string  `json:"description"`
	ReturnURL   string  `json:"return_url"`  // Куда вернуть пользователя после оплаты
}

// CreatePaymentResponse ответ при создании платежа
type CreatePaymentResponse struct {
	OrderID       int32  `json:"order_id"`
	PaymentURL    string `json:"payment_url"`    // URL для перенаправления на оплату
	Status        string `json:"status"`
	AmountRub     float64 `json:"amount_rub"`
}

// CreatePayment создает заказ на оплату и платеж в ЮКассе
func (s *PaymentService) CreatePayment(ctx context.Context, accountID int64, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
	// Проверяем аккаунт
	account, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}
	if account.Status != "active" {
		return nil, fmt.Errorf("account is not active")
	}

	if req.AmountRub <= 0 {
		return nil, fmt.Errorf("invalid amount")
	}

	// Создаем заказ в БД
	metadata := map[string]interface{}{
		"account_id": accountID,
		"created_at": time.Now().Format(time.RFC3339),
	}

	order := &models.PaymentOrder{
		AccountID: accountID,
		AmountRub: req.AmountRub,
		Status:    "pending",
		Metadata:  metadata,
	}

	if err := s.repo.CreatePaymentOrder(ctx, order); err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Создаем платеж в ЮКассе
	paymentURL, err := s.createYooKassaPayment(ctx, order, req)
	if err != nil {
		// Обновляем статус заказа на failed
		order.Status = "failed"
		if err := s.repo.UpdatePaymentOrder(ctx, order); err != nil {
			log.Printf("[CreatePayment] failed to update order status: %v", err)
		}
		return nil, fmt.Errorf("failed to create yookassa payment: %w", err)
	}

	return &CreatePaymentResponse{
		OrderID:    order.ID,
		PaymentURL: paymentURL,
		Status:     "pending",
		AmountRub:  req.AmountRub,
	}, nil
}

// createYooKassaPayment создает платеж в ЮКассе
func (s *PaymentService) createYooKassaPayment(ctx context.Context, order *models.PaymentOrder, req *CreatePaymentRequest) (string, error) {
	// В MVP делаем заглушку - в реальности здесь HTTP запрос к ЮКассе
	// TODO: реализовать реальный HTTP запрос к API ЮКассы с использованием payload:
	/*
	payload := map[string]interface{}{
		"amount": map[string]interface{}{
			"value":    fmt.Sprintf("%.2f", req.AmountRub),
			"currency": "RUB",
		},
		"confirmation": map[string]interface{}{
			"type":       "redirect",
			"return_url": req.ReturnURL,
		},
		"capture":     true,
		"description": req.Description,
		"metadata": map[string]interface{}{
			"order_id":   order.ID,
			"account_id": order.AccountID,
		},
	}
	*/
	
	// Заглушка для MVP - имитируем успешное создание
	yookassaID := fmt.Sprintf("mock_%d_%d", order.ID, time.Now().Unix())
	order.YookassaPaymentID = &yookassaID
	if err := s.repo.UpdatePaymentOrder(ctx, order); err != nil {
		log.Printf("[createYooKassaPayment] failed to update order: %v", err)
	}

	// Возвращаем mock URL для тестирования
	return fmt.Sprintf("https://yookassa.ru/payments/%s", yookassaID), nil
}

// GetPaymentOrder возвращает информацию о заказе
func (s *PaymentService) GetPaymentOrder(ctx context.Context, orderID int32) (*models.PaymentOrder, error) {
	return s.repo.GetPaymentOrder(ctx, orderID)
}

// ProcessExpiredPayments обрабатывает просроченные платежи (cron job)
// Вызывается периодически для перевода старых pending платежей в failed
func (s *PaymentService) ProcessExpiredPayments(ctx context.Context, timeout time.Duration) (int, error) {
	// Находим все pending платежи, которые висят дольше timeout
	expiredOrders, err := s.repo.GetExpiredPendingPayments(ctx, timeout)
	if err != nil {
		return 0, fmt.Errorf("failed to get expired payments: %w", err)
	}

	processed := 0
	for _, order := range expiredOrders {
		// Проверяем еще раз статус (может webhook успел обработать)
		freshOrder, err := s.repo.GetPaymentOrder(ctx, order.ID)
		if err != nil {
			continue // Пропускаем при ошибке
		}
		if freshOrder.Status != "pending" {
			continue // Уже обработан webhook'ом
		}

		// Переводим в failed
		order.Status = "failed"
		if err := s.repo.UpdatePaymentOrder(ctx, order); err != nil {
			continue // Логируем ошибку, но продолжаем
		}
		processed++
	}

	return processed, nil
}
