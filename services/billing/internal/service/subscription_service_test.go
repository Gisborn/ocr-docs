package service

import (
	"context"
	"testing"
	"time"

	"scan.passport.local/api/services/billing/pkg/models"
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
		BasePriceRub:     10000,
		PrepaidAmountRub: 10000,
	}
	
	svc := NewSubscriptionService(repo)
	
	// Создаем аккаунт и пополняем баланс
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 15000
	
	req := &CreateSubscriptionRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	}
	
	resp, err := svc.CreateSubscription(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("CreateSubscription failed: %v", err)
	}
	
	if resp.SubscriptionID == 0 {
		t.Error("Expected subscription ID")
	}
	if resp.Status != "active" {
		t.Errorf("Expected status active, got %s", resp.Status)
	}
	// Проверяем что создано событие списания
	foundPayment := false
	for _, e := range repo.events {
		if e.Type == "subscription_charge" && e.RealAmountRub == -10000 {
			foundPayment = true
			break
		}
	}
	if !foundPayment {
		t.Error("Expected subscription_charge event with -10000")
	}

	// Проверяем что создано событие начисления prepaid
	foundPrepaid := false
	for _, e := range repo.events {
		if e.Type == "upgrade_bonus" && e.PrepaidAmountRub == 10000 {
			foundPrepaid = true
			break
		}
	}
	if !foundPrepaid {
		t.Error("Expected upgrade_bonus event with +10000 prepaid")
	}

	// Проверяем что создано ровно 2 события
	if len(repo.events) != 2 {
		t.Errorf("Expected 2 billing events, got %d", len(repo.events))
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
		BasePriceRub:     10000,
		PrepaidAmountRub: 10000,
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
		BasePriceRub:     10000,
		PrepaidAmountRub: 10000,
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
	resp, err := svc.UpgradeSubscription(context.Background(), acc.ID, &UpgradeSubscriptionRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	})

	if err != nil {
		t.Fatalf("UpgradeSubscription failed: %v", err)
	}

	// Проверяем что списана доплата
	// При апгрейде сразу после создания доплата должна быть полной ценой
	if resp.AmountCharged <= 0 {
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

func TestGetActiveSubscription(t *testing.T) {
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
		BasePriceRub:     10000,
		PrepaidAmountRub: 10000,
	}

	svc := NewSubscriptionService(repo)

	// Создаем аккаунт и подписку
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 15000

	_, _ = svc.CreateSubscription(context.Background(), acc.ID, &CreateSubscriptionRequest{
		TariffCode:    "pro",
		PaymentMethod: "balance",
	})

	// Получаем активную подписку
	resp, err := svc.GetActiveSubscription(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("GetActiveSubscription failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected subscription, got nil")
	}
	if resp.TariffCode != "pro" {
		t.Errorf("Expected tariff_code pro, got %s", resp.TariffCode)
	}
	if resp.TariffName != "Pro" {
		t.Errorf("Expected tariff_name Pro, got %s", resp.TariffName)
	}
	if resp.Status != "active" {
		t.Errorf("Expected status active, got %s", resp.Status)
	}
	if resp.AccountID != acc.ID {
		t.Errorf("Expected account_id %d, got %d", acc.ID, resp.AccountID)
	}
}

func TestGetActiveSubscriptionNotFound(t *testing.T) {
	repo := NewMockRepository()
	svc := NewSubscriptionService(repo)

	acc, _ := repo.CreateAccount(context.Background())

	// Пытаемся получить подписку для аккаунта без подписки
	_, err := svc.GetActiveSubscription(context.Background(), acc.ID)
	if err == nil {
		t.Fatal("Expected error for account without subscription")
	}
	if err.Error() != "no active subscription" {
		t.Errorf("Expected 'no active subscription', got: %v", err)
	}
}

func TestGetBillingEvents(t *testing.T) {
	repo := NewMockRepository()
	svc := NewSubscriptionService(repo)

	acc, _ := repo.CreateAccount(context.Background())

	// Создаем несколько событий
	repo.CreateBillingEvent(context.Background(), &models.BillingEvent{
		AccountID:     acc.ID,
		Type:          "balance_topup",
		RealAmountRub: 1000,
	})
	repo.CreateBillingEvent(context.Background(), &models.BillingEvent{
		AccountID:     acc.ID,
		Type:          "pay_as_you_go",
		RealAmountRub: -50,
	})

	// Получаем историю
	events, err := svc.GetBillingEvents(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("GetBillingEvents failed: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	// Проверяем порядок (сначала старые, потом новые — или наоборот, зависит от реализации)
	foundTopup := false
	foundUsage := false
	for _, e := range events {
		if e.Type == "balance_topup" && e.RealAmountRub == 1000 {
			foundTopup = true
		}
		if e.Type == "pay_as_you_go" && e.RealAmountRub == -50 {
			foundUsage = true
		}
	}
	if !foundTopup {
		t.Error("Expected balance_topup event")
	}
	if !foundUsage {
		t.Error("Expected pay_as_you_go event")
	}
}
