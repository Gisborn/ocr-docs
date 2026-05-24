package service

import (
	"context"
	"testing"

	"scan.passport.local/api/services/billing/pkg/models"
)

func TestReserveWithSufficientBalance(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)

	// Создаем аккаунт и пополняем баланс
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 1000

	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_001",
	}

	resp, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}

	if !resp.Reserved {
		t.Error("Expected reserved to be true")
	}
	if resp.AmountRub <= 0 {
		t.Error("Expected positive amount")
	}
}

func TestReserveWithInsufficientBalance(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)

	// Создаем аккаунт с нулевым балансом
	acc, _ := repo.CreateAccount(context.Background())

	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_002",
	}

	_, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != ErrInsufficientBalance {
		t.Fatalf("Expected ErrInsufficientBalance, got: %v", err)
	}
}

func TestCommit(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)

	// Создаем аккаунт и резерв
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 1000

	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_003",
	}

	svc.Reserve(context.Background(), acc.ID, req)

	// Проверяем что резерв создан
	if _, ok := repo.reservations["req_003"]; !ok {
		t.Fatal("Reservation not created")
	}

	// Коммитим
	err := svc.Commit(context.Background(), "req_003")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Проверяем что резерв удален и баланс обновлен
	if _, ok := repo.reservations["req_003"]; ok {
		t.Error("Reservation should be deleted after commit")
	}

	// Проверяем что создано событие
	if len(repo.events) == 0 {
		t.Error("Expected billing event to be created")
	}
}

func TestRollback(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)

	// Создаем аккаунт и резерв
	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 1000

	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_004",
	}

	svc.Reserve(context.Background(), acc.ID, req)

	// Откатываем
	err := svc.Rollback(context.Background(), "req_004", "test reason")
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Проверяем что резерв удален
	if _, ok := repo.reservations["req_004"]; ok {
		t.Error("Reservation should be deleted after rollback")
	}

	// Проверяем что событие НЕ создано (списания не было)
	if len(repo.events) > 0 {
		t.Error("Expected no billing events after rollback")
	}
}

func TestReserveBlockedAccount(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)

	acc, _ := repo.CreateAccount(context.Background())
	repo.accounts[acc.ID].Status = "blocked"
	repo.balances[acc.ID].RealBalanceRub = 1000

	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_blocked",
	}

	_, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != ErrAccountBlocked {
		t.Fatalf("Expected ErrAccountBlocked, got: %v", err)
	}
}

func TestReserveArchivedAccount(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)

	acc, _ := repo.CreateAccount(context.Background())
	repo.accounts[acc.ID].Status = "archived"
	repo.balances[acc.ID].RealBalanceRub = 1000

	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_archived",
	}

	_, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != ErrAccountArchived {
		t.Fatalf("Expected ErrAccountArchived, got: %v", err)
	}
}

func TestReserveIdempotency(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)

	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 1000

	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_idem",
	}

	resp1, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("First reserve failed: %v", err)
	}

	resp2, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("Second reserve failed: %v", err)
	}

	if resp1.AmountRub != resp2.AmountRub {
		t.Errorf("Idempotency failed: %.2f != %.2f", resp1.AmountRub, resp2.AmountRub)
	}

	// Должен быть только 1 резерв
	count := 0
	for _, r := range repo.reservations {
		if r.RequestID == "req_idem" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 reservation, got %d", count)
	}
}

func TestReserveWithPrepaidSubscription(t *testing.T) {
	repo := NewMockRepository()
	repo.SeedTestData()
	svc := NewBillingService(repo)

	acc, _ := repo.CreateAccount(context.Background())
	repo.balances[acc.ID].RealBalanceRub = 2000

	// Создаем активную подписку pro
	sub := &models.Subscription{
		AccountID:       acc.ID,
		TariffVersionID: 2,
		Status:          "active",
		InitialPrepaidRub: 500,
	}
	repo.CreateSubscription(context.Background(), sub)

	req := &ReserveRequest{
		ServiceID: "passport_rf",
		RequestID: "req_prepaid",
	}

	resp, err := svc.Reserve(context.Background(), acc.ID, req)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}

	// Для pro тарифа с prepaid должен использоваться prepaid (цена 0)
	if resp.ChargeType != "prepaid" {
		t.Errorf("Expected charge_type=prepaid, got %s", resp.ChargeType)
	}
}

func TestCommitNonExistentReservation(t *testing.T) {
	repo := NewMockRepository()
	svc := NewBillingService(repo)

	err := svc.Commit(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("Expected error for nonexistent reservation")
	}
}
