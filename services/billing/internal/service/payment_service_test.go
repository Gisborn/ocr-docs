package service

import (
	"context"
	"testing"
	"time"

	"scan.passport.local/api/services/billing/pkg/models"
)

func TestCreatePayment(t *testing.T) {
	repo := NewMockRepository()
	svc := NewPaymentService(repo, nil, "")

	// Создаем аккаунт
	acc, _ := repo.CreateAccount(context.Background())

	req := &CreatePaymentRequest{
		AmountRub:   1000,
		Description: "Test payment",
		ReturnURL:   "https://example.com/return",
	}

	resp, err := svc.CreatePayment(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("CreatePayment failed: %v", err)
	}

	if resp.OrderID == 0 {
		t.Error("Expected order ID")
	}
	if resp.Status != "pending" {
		t.Errorf("Expected status pending, got %s", resp.Status)
	}
	if resp.AmountRub != 1000 {
		t.Errorf("Expected amount 1000, got %f", resp.AmountRub)
	}
	if resp.PaymentURL == "" {
		t.Error("Expected payment URL")
	}

	// Проверяем что заказ создан в БД
	order, err := repo.GetPaymentOrder(context.Background(), resp.OrderID)
	if err != nil {
		t.Fatalf("GetPaymentOrder failed: %v", err)
	}
	if order.Status != "pending" {
		t.Errorf("Expected order status pending, got %s", order.Status)
	}
}

func TestProcessExpiredPayments(t *testing.T) {
	repo := NewMockRepository()
	svc := NewPaymentService(repo, nil, "")

	// Создаем аккаунт
	acc, _ := repo.CreateAccount(context.Background())

	// Создаем старый pending платеж (симулируем создание в прошлом)
	oldOrder := &models.PaymentOrder{
		AccountID: acc.ID,
		AmountRub: 1000,
		Status:    "pending",
		CreatedAt: time.Now().Add(-2 * time.Hour), // 2 часа назад
	}
	repo.CreatePaymentOrder(context.Background(), oldOrder)

	// Создаем свежий pending платеж
	freshOrder := &models.PaymentOrder{
		AccountID: acc.ID,
		AmountRub: 2000,
		Status:    "pending",
		CreatedAt: time.Now().Add(-5 * time.Minute), // 5 минут назад
	}
	repo.CreatePaymentOrder(context.Background(), freshOrder)

	// Запускаем обработку с таймаутом 1 час
	processed, err := svc.ProcessExpiredPayments(context.Background(), 1*time.Hour)
	if err != nil {
		t.Fatalf("ProcessExpiredPayments failed: %v", err)
	}

	if processed != 1 {
		t.Errorf("Expected 1 expired payment processed, got %d", processed)
	}

	// Проверяем что старый платеж стал failed
	oldOrder, _ = repo.GetPaymentOrder(context.Background(), oldOrder.ID)
	if oldOrder.Status != "failed" {
		t.Errorf("Expected old order status failed, got %s", oldOrder.Status)
	}

	// Проверяем что свежий платеж остался pending
	freshOrder, _ = repo.GetPaymentOrder(context.Background(), freshOrder.ID)
	if freshOrder.Status != "pending" {
		t.Errorf("Expected fresh order status pending, got %s", freshOrder.Status)
	}
}
