package service

import (
	"context"
	"testing"
	"time"

	"github.com/api-scan/api-scan/services/billing/pkg/models"
)

func TestCreateSubscription(t *testing.T) {
	repo := NewMockRepository()
	
	// Создаем тестовый тариф
	repo.tariffs[1] = &models.Tariff{
		ID:   1,
		Code: "pro",
		Name: "Pro",
	}
	repo.tariffVersions[1] = &models.TariffVersion{
		ID:               1,
		TariffID:         1,
		ValidFrom:        time.Now().AddDate(-1, 0, 0),
		DurationDays:     30,
		BasePriceRub:     20000,
		PrepaidAmountRub: 6000,
	}
	
	svc := NewSubscriptionService(repo)
	
	// Создаем аккаунт и пополняем баланс
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 25000
	
	req := &CreateSubscriptionRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	}
	
	resp, err := svc.CreateSubscription(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("CreateSubscription failed: %v", err)
	}
	
	if resp.ID == 0 {
		t.Error("Expected subscription ID")
	}
	if resp.Status != "active" {
		t.Errorf("Expected status active, got %s", resp.Status)
	}
	if resp.TariffCode != "pro" {
		t.Errorf("Expected tariff pro, got %s", resp.TariffCode)
	}
	
	// Проверяем что создано событие списания
	foundPayment := false
	for _, e := range repo.events {
		if e.Type == "subscription_payment" && e.RealAmountRub == -20000 {
			foundPayment = true
			break
		}
	}
	if !foundPayment {
		t.Error("Expected subscription_payment event with -20000")
	}
	
	// Проверяем что созданы события
	if len(repo.events) < 2 {
		t.Error("Expected at least 2 billing events (payment + prepaid)")
	}
}

func TestCreateSubscriptionInsufficientBalance(t *testing.T) {
	repo := NewMockRepository()
	
	repo.tariffs[1] = &models.Tariff{
		ID:   1,
		Code: "pro",
		Name: "Pro",
	}
	repo.tariffVersions[1] = &models.TariffVersion{
		ID:               1,
		TariffID:         1,
		ValidFrom:        time.Now().AddDate(-1, 0, 0),
		DurationDays:     30,
		BasePriceRub:     20000,
		PrepaidAmountRub: 6000,
	}
	
	svc := NewSubscriptionService(repo)
	
	// Создаем аккаунт с недостаточным балансом
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 1000
	
	req := &CreateSubscriptionRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	}
	
	_, err := svc.CreateSubscription(context.Background(), acc.ID, req)
	if err != ErrInsufficientBalance {
		t.Fatalf("Expected ErrInsufficientBalance, got: %v", err)
	}
}

func TestUpgradeSubscription(t *testing.T) {
	repo := NewMockRepository()
	
	// Создаем тарифы
	repo.tariffs[1] = &models.Tariff{ID: 1, Code: "basic", Name: "Basic"}
	repo.tariffs[2] = &models.Tariff{ID: 2, Code: "pro", Name: "Pro"}
	
	repo.tariffVersions[1] = &models.TariffVersion{
		ID:               1,
		TariffID:         1,
		ValidFrom:        time.Now().AddDate(-1, 0, 0),
		DurationDays:     30,
		BasePriceRub:     0,
		PrepaidAmountRub: 0,
	}
	repo.tariffVersions[2] = &models.TariffVersion{
		ID:               2,
		TariffID:         2,
		ValidFrom:        time.Now().AddDate(-1, 0, 0),
		DurationDays:     30,
		BasePriceRub:     20000,
		PrepaidAmountRub: 6000,
	}
	
	svc := NewSubscriptionService(repo)
	
	// Создаем аккаунт с достаточным балансом
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 50000
	
	// Сначала создаем базовую подписку
	_, _ = svc.CreateSubscription(context.Background(), acc.ID, &CreateSubscriptionRequest{
		TariffCode:    "basic",
		PaymentMethod: "balance",
	})
	
	// Апгрейдим до pro
	resp, err := svc.Upgrade(context.Background(), acc.ID, &UpgradeRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	})
	
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}
	
	if resp.PreviousTariff != "basic" {
		t.Errorf("Expected previous tariff basic, got %s", resp.PreviousTariff)
	}
	if resp.NewTariff != "pro" {
		t.Errorf("Expected new tariff pro, got %s", resp.NewTariff)
	}
	
	// Проверяем что списана доплата
	// При апгрейде сразу после создания доплата должна быть полной ценой
	if resp.TotalChargeRub <= 0 {
		t.Error("Expected positive total charge")
	}
}

func TestGetBalance(t *testing.T) {
	repo := NewMockRepository()
	svc := NewSubscriptionService(repo)
	
	// Создаем аккаунт и пополняем
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 10000
	repo.balances[acc.ID].PrepaidBalanceRub = 5000
	
	// Добавляем событие
	repo.CreateBillingEvent(context.Background(), &models.BillingEvent{
		AccountID:     acc.ID,
		Type:          "balance_topup",
		RealAmountRub: 5000,
	})
	
	balance, err := svc.GetBalance(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	
	// Проверяем что баланс учитывает события
	expectedReal := 15000.0 // 10000 + 5000
	if balance.RealBalanceRub != expectedReal {
		t.Errorf("Expected real balance %f, got %f", expectedReal, balance.RealBalanceRub)
	}
}

func TestIdempotentReserve(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)
	
	// Создаем аккаунт
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 1000
	
	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_dup",
	}
	
	// Первый вызов
	resp1, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("First reserve failed: %v", err)
	}
	
	// Второй вызов с тем же request_id (идемпотентность)
	resp2, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("Second reserve failed: %v", err)
	}
	
	// Проверяем что результаты одинаковые
	if resp1.TransactionID != resp2.TransactionID {
		t.Error("Expected same transaction ID for idempotent request")
	}
	if resp1.AmountRub != resp2.AmountRub {
		t.Error("Expected same amount for idempotent request")
	}
	
	// Проверяем что создан только один резерв
	count := 0
	for _, r := range repo.reservations {
		if r.RequestID == "req_dup" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 reservation, got %d", count)
	}
}
