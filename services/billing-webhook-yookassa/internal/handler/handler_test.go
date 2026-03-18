package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scan.passport.local/api/pkg/billing"
)

// MockRepository для тестирования
type MockRepository struct {
	orders map[int32]*billing.PaymentOrder
	events []*billing.BillingEvent
}

func NewMockRepository() *MockRepository {
	return &MockRepository{
		orders: make(map[int32]*billing.PaymentOrder),
		events: make([]*billing.BillingEvent, 0),
	}
}

func (m *MockRepository) GetPaymentOrderByYookassaID(ctx context.Context, paymentID string) (*billing.PaymentOrder, error) {
	for _, o := range m.orders {
		if o.YookassaPaymentID != nil && *o.YookassaPaymentID == paymentID {
			return o, nil
		}
	}
	return nil, nil
}

func (m *MockRepository) UpdatePaymentOrder(ctx context.Context, order *billing.PaymentOrder) error {
	m.orders[order.ID] = order
	return nil
}

func (m *MockRepository) CreateBillingEvent(ctx context.Context, event *billing.BillingEvent) error {
	event.ID = int64(len(m.events) + 1)
	m.events = append(m.events, event)
	return nil
}

func TestYookassaWebhook_Success(t *testing.T) {
	repo := NewMockRepository()
	handler := NewHandler(repo, "", "")

	// Создаем тестовый заказ
	yookassaID := "test_payment_123"
	order := &billing.PaymentOrder{
		ID:                1,
		AccountID:         100,
		AmountRub:         5000,
		Status:            "pending",
		YookassaPaymentID: &yookassaID,
		CreatedAt:         time.Now(),
	}
	repo.orders[1] = order

	// Формируем webhook
	webhook := map[string]interface{}{
		"type":  "notification",
		"event": "payment.succeeded",
		"object": map[string]interface{}{
			"id":     "test_payment_123",
			"status": "succeeded",
			"amount": map[string]interface{}{
				"value":    "5000.00",
				"currency": "RUB",
			},
			"metadata": map[string]string{
				"order_id":   "1",
				"account_id": "100",
			},
		},
	}

	body, _ := json.Marshal(webhook)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/yookassa", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.YookassaWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Проверяем что заказ обновлен
	updatedOrder := repo.orders[1]
	if updatedOrder.Status != "completed" {
		t.Errorf("Expected status completed, got %s", updatedOrder.Status)
	}
	if updatedOrder.PaidAt == nil {
		t.Error("Expected PaidAt to be set")
	}

	// Проверяем что создано событие
	if len(repo.events) != 1 {
		t.Errorf("Expected 1 billing event, got %d", len(repo.events))
	}
	if repo.events[0].Type != "balance_topup" {
		t.Errorf("Expected event type balance_topup, got %s", repo.events[0].Type)
	}
	if repo.events[0].RealAmountRub != 5000 {
		t.Errorf("Expected amount 5000, got %f", repo.events[0].RealAmountRub)
	}
}

func TestYookassaWebhook_Canceled(t *testing.T) {
	repo := NewMockRepository()
	handler := NewHandler(repo, "", "")

	// Создаем тестовый заказ
	yookassaID := "test_payment_456"
	order := &billing.PaymentOrder{
		ID:                2,
		AccountID:         200,
		AmountRub:         3000,
		Status:            "pending",
		YookassaPaymentID: &yookassaID,
		CreatedAt:         time.Now(),
	}
	repo.orders[2] = order

	// Формируем webhook отмены
	webhook := map[string]interface{}{
		"type":  "notification",
		"event": "payment.canceled",
		"object": map[string]interface{}{
			"id":     "test_payment_456",
			"status": "canceled",
		},
	}

	body, _ := json.Marshal(webhook)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/yookassa", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.YookassaWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Проверяем что заказ отмечен как failed
	updatedOrder := repo.orders[2]
	if updatedOrder.Status != "failed" {
		t.Errorf("Expected status failed, got %s", updatedOrder.Status)
	}

	// Проверяем что событие НЕ создано
	if len(repo.events) != 0 {
		t.Error("Expected no billing events for canceled payment")
	}
}

func TestYookassaWebhook_Idempotent(t *testing.T) {
	repo := NewMockRepository()
	handler := NewHandler(repo, "", "")

	yookassaID := "test_payment_789"
	order := &billing.PaymentOrder{
		ID:                3,
		AccountID:         300,
		AmountRub:         1000,
		Status:            "pending",
		YookassaPaymentID: &yookassaID,
		CreatedAt:         time.Now(),
	}
	repo.orders[3] = order

	webhook := map[string]interface{}{
		"type":  "notification",
		"event": "payment.succeeded",
		"object": map[string]interface{}{
			"id":     "test_payment_789",
			"status": "succeeded",
			"amount": map[string]interface{}{
				"value":    "1000.00",
				"currency": "RUB",
			},
		},
	}

	body, _ := json.Marshal(webhook)

	// Первый вызов
	req1 := httptest.NewRequest(http.MethodPost, "/webhooks/yookassa", bytes.NewReader(body))
	rec1 := httptest.NewRecorder()
	handler.YookassaWebhook(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("First call failed: %d", rec1.Code)
	}

	eventsAfterFirst := len(repo.events)

	// Второй вызов (тот же webhook)
	req2 := httptest.NewRequest(http.MethodPost, "/webhooks/yookassa", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	handler.YookassaWebhook(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("Second call failed: %d", rec2.Code)
	}

	// Проверяем что событий не добавилось
	if len(repo.events) != eventsAfterFirst {
		t.Errorf("Idempotency broken: expected %d events, got %d", eventsAfterFirst, len(repo.events))
	}
}

func TestYookassaWebhook_RecoverFromFailed(t *testing.T) {
	repo := NewMockRepository()
	handler := NewHandler(repo, "", "")

	// Заказ уже отмечен как failed (например, по таймауту)
	yookassaID := "test_payment_recover"
	order := &billing.PaymentOrder{
		ID:                4,
		AccountID:         400,
		AmountRub:         2000,
		Status:            "failed",
		YookassaPaymentID: &yookassaID,
		CreatedAt:         time.Now().Add(-2 * time.Hour),
	}
	repo.orders[4] = order

	// Приходит webhook об успешной оплате
	webhook := map[string]interface{}{
		"type":  "notification",
		"event": "payment.succeeded",
		"object": map[string]interface{}{
			"id":     "test_payment_recover",
			"status": "succeeded",
			"amount": map[string]interface{}{
				"value":    "2000.00",
				"currency": "RUB",
			},
		},
	}

	body, _ := json.Marshal(webhook)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/yookassa", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.YookassaWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Проверяем что заказ восстановлен и завершен
	updatedOrder := repo.orders[4]
	if updatedOrder.Status != "completed" {
		t.Errorf("Expected status completed after recovery, got %s", updatedOrder.Status)
	}

	// Проверяем что баланс пополнен
	if len(repo.events) != 1 {
		t.Errorf("Expected 1 billing event after recovery, got %d", len(repo.events))
	}
}

func TestYookassaWebhook_InvalidMethod(t *testing.T) {
	repo := NewMockRepository()
	handler := NewHandler(repo, "", "")

	req := httptest.NewRequest(http.MethodGet, "/webhooks/yookassa", nil)
	rec := httptest.NewRecorder()

	handler.YookassaWebhook(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestYookassaWebhook_InvalidJSON(t *testing.T) {
	repo := NewMockRepository()
	handler := NewHandler(repo, "", "")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/yookassa", bytes.NewReader([]byte("invalid json")))
	rec := httptest.NewRecorder()

	handler.YookassaWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestYookassaWebhook_InvalidType(t *testing.T) {
	repo := NewMockRepository()
	handler := NewHandler(repo, "", "")

	webhook := map[string]interface{}{
		"type":  "invalid",
		"event": "payment.succeeded",
	}

	body, _ := json.Marshal(webhook)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/yookassa", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.YookassaWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestYookassaWebhook_IPWhitelist(t *testing.T) {
	repo := NewMockRepository()
	// Настраиваем whitelist с тестовым IP
	handler := NewHandler(repo, "", "127.0.0.1/8")

	webhook := map[string]interface{}{
		"type":  "notification",
		"event": "payment.succeeded",
		"object": map[string]interface{}{
			"id":     "test_payment_999",
			"status": "succeeded",
			"amount": map[string]interface{}{
				"value":    "100.00",
				"currency": "RUB",
			},
		},
	}

	body, _ := json.Marshal(webhook)
	
	// Запрос с разрешенного IP (localhost)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/yookassa", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	handler.YookassaWebhook(rec, req)

	// Должен пройти (200 или 404 если заказ не найден)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 200 or 404, got %d", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	repo := NewMockRepository()
	handler := NewHandler(repo, "", "")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("Expected status ok, got %s", resp["status"])
	}
}
