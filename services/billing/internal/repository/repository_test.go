package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"scan.passport.local/api/pkg/testdb"
	"scan.passport.local/api/services/billing/pkg/models"
)

func setupBillingRepo(t *testing.T) (*PostgresRepository, *pgxpool.Pool) {
	pool := testdb.MustPool(t, testdb.DefaultBillingURL())
	testdb.ApplyMigrations(t, pool, "../../../../migrations/billing")
	testdb.Cleanup(t, pool,
		"reservations", "billing_events", "subscriptions", "payment_orders",
		"tariff_service_prices", "tariff_versions", "tariffs", "services",
		"balance_snapshots", "accounts",
	)
	return NewPostgresRepository(pool), pool
}

func TestPostgresRepository_CreateAccount(t *testing.T) {
	repo, _ := setupBillingRepo(t)
	ctx := context.Background()

	acc, err := repo.CreateAccount(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acc.ID == 0 {
		t.Fatal("expected account id")
	}
	if acc.Status != "active" {
		t.Fatalf("expected status active, got %s", acc.Status)
	}

	// Verify balance snapshot created
	snap, err := repo.GetAccountBalance(ctx, acc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.RealBalanceRub != 0 {
		t.Fatalf("expected real balance 0, got %f", snap.RealBalanceRub)
	}
	if snap.PrepaidBalanceRub != 0 {
		t.Fatalf("expected prepaid balance 0, got %f", snap.PrepaidBalanceRub)
	}
}

func TestPostgresRepository_GetAccount(t *testing.T) {
	repo, _ := setupBillingRepo(t)
	ctx := context.Background()

	acc, _ := repo.CreateAccount(ctx)
	found, err := repo.GetAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != acc.ID {
		t.Fatalf("expected id %d, got %d", acc.ID, found.ID)
	}
}

func TestPostgresRepository_UpdateBalanceSnapshot(t *testing.T) {
	repo, _ := setupBillingRepo(t)
	ctx := context.Background()

	acc, _ := repo.CreateAccount(ctx)
	err := repo.UpdateBalanceSnapshot(ctx, &models.BalanceSnapshot{
		AccountID:         acc.ID,
		RealBalanceRub:    100.5,
		PrepaidBalanceRub: 20.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snap, _ := repo.GetAccountBalance(ctx, acc.ID)
	if snap.RealBalanceRub != 100.5 {
		t.Fatalf("expected real balance 100.5, got %f", snap.RealBalanceRub)
	}
	if snap.PrepaidBalanceRub != 20.0 {
		t.Fatalf("expected prepaid balance 20.0, got %f", snap.PrepaidBalanceRub)
	}
}

func TestPostgresRepository_Reservations(t *testing.T) {
	repo, _ := setupBillingRepo(t)
	ctx := context.Background()

	acc, _ := repo.CreateAccount(ctx)

	res := &models.Reservation{
		AccountID:  acc.ID,
		RequestID:  "req_001",
		AmountRub:  50.0,
		ChargeType: "real",
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	err := repo.CreateReservation(ctx, res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ID == 0 {
		t.Fatal("expected reservation id")
	}

	// Get by request_id
	found, err := repo.GetReservation(ctx, "req_001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.RequestID != "req_001" {
		t.Fatalf("expected request_id req_001, got %s", found.RequestID)
	}

	// Get active
	active, err := repo.GetActiveReservations(ctx, acc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active reservation, got %d", len(active))
	}

	// Delete
	err = repo.DeleteReservation(ctx, "req_001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repo.GetReservation(ctx, "req_001")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestPostgresRepository_DeleteExpiredReservations(t *testing.T) {
	repo, pool := setupBillingRepo(t)
	ctx := context.Background()

	acc, _ := repo.CreateAccount(ctx)
	pool.Exec(ctx,
		`INSERT INTO reservations (account_id, request_id, amount_rub, charge_type, created_at, expires_at)
		 VALUES ($1, 'expired', 10.0, 'real', NOW(), NOW() - INTERVAL '1 minute')`,
		acc.ID)

	err := repo.DeleteExpiredReservations(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM reservations WHERE request_id = 'expired'`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 expired reservations, got %d", count)
	}
}

func TestPostgresRepository_BillingEvents(t *testing.T) {
	repo, _ := setupBillingRepo(t)
	ctx := context.Background()

	acc, _ := repo.CreateAccount(ctx)
	event := &models.BillingEvent{
		AccountID:        acc.ID,
		Type:             "balance_topup",
		RealAmountRub:    100.0,
		PrepaidAmountRub: 0,
		RequestID:        strPtr("req_1"),
	}
	err := repo.CreateBillingEvent(ctx, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.ID == 0 {
		t.Fatal("expected event id")
	}

	since := time.Now().Add(-time.Hour)
	events, err := repo.GetBillingEventsSince(ctx, acc.ID, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestPostgresRepository_Subscriptions(t *testing.T) {
	repo, pool := setupBillingRepo(t)
	ctx := context.Background()

	acc, _ := repo.CreateAccount(ctx)

	// Seed tariff
	var tariffID int16
	pool.QueryRow(ctx, `INSERT INTO tariffs (code, name) VALUES ('start', 'Start') RETURNING id`).Scan(&tariffID)
	var tvID int32
	pool.QueryRow(ctx,
		`INSERT INTO tariff_versions (tariff_id, duration_days, base_price_rub, prepaid_amount_rub)
		 VALUES ($1, 30, 100.00, 50.00) RETURNING id`, tariffID).Scan(&tvID)

	sub := &models.Subscription{
		AccountID:         acc.ID,
		TariffVersionID:   tvID,
		Status:            "active",
		StartedAt:         time.Now(),
		ExpiresAt:         time.Now().Add(30 * 24 * time.Hour),
		InitialPrepaidRub: 50.0,
		AutoRenew:         false,
	}
	err := repo.CreateSubscription(ctx, sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.ID == 0 {
		t.Fatal("expected subscription id")
	}

	// Get active
	active, err := repo.GetActiveSubscription(ctx, acc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active == nil {
		t.Fatal("expected active subscription")
	}
	if active.ID != sub.ID {
		t.Fatalf("expected id %d, got %d", sub.ID, active.ID)
	}

	// Get by ID
	found, err := repo.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != sub.ID {
		t.Fatalf("expected id %d, got %d", sub.ID, found.ID)
	}

	// Update
	sub.Status = "expired"
	err = repo.UpdateSubscription(ctx, sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No longer active
	active, _ = repo.GetActiveSubscription(ctx, acc.ID)
	if active != nil {
		t.Fatal("expected no active subscription after status change")
	}
}

func TestPostgresRepository_Tariffs(t *testing.T) {
	repo, pool := setupBillingRepo(t)
	ctx := context.Background()

	var tariffID int16
	pool.QueryRow(ctx, `INSERT INTO tariffs (code, name, description) VALUES ('pro', 'Pro', 'Pro tariff') RETURNING id`).Scan(&tariffID)
	var tvID int32
	pool.QueryRow(ctx,
		`INSERT INTO tariff_versions (tariff_id, valid_from, duration_days, base_price_rub, prepaid_amount_rub)
		 VALUES ($1, NOW() - INTERVAL '1 day', 30, 500.00, 200.00) RETURNING id`, tariffID).Scan(&tvID)
	pool.Exec(ctx, `INSERT INTO services (id, name) VALUES ('ocr', 'OCR')`)
	pool.Exec(ctx,
		`INSERT INTO tariff_service_prices (tariff_version_id, service_id, included_price_rub, overage_price_rub)
		 VALUES ($1, 'ocr', 5.00, 10.00)`, tvID)

	// GetTariff
	tariff, err := repo.GetTariff(ctx, tariffID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tariff.Code != "pro" {
		t.Fatalf("expected code pro, got %s", tariff.Code)
	}

	// GetTariffVersion
	tv, err := repo.GetTariffVersion(ctx, tvID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tv.TariffID != tariffID {
		t.Fatalf("expected tariff_id %d, got %d", tariffID, tv.TariffID)
	}

	// GetTariffVersionByCode
	tv2, err := repo.GetTariffVersionByCode(ctx, "pro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tv2.ID != tvID {
		t.Fatalf("expected version id %d, got %d", tvID, tv2.ID)
	}

	// GetServicePrice
	price, err := repo.GetServicePrice(ctx, tvID, "ocr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if price.IncludedPriceRub != 5.0 {
		t.Fatalf("expected included price 5.0, got %f", price.IncludedPriceRub)
	}
	if price.OveragePriceRub != 10.0 {
		t.Fatalf("expected overage price 10.0, got %f", price.OveragePriceRub)
	}
}

func TestPostgresRepository_PaymentOrders(t *testing.T) {
	repo, _ := setupBillingRepo(t)
	ctx := context.Background()

	acc, _ := repo.CreateAccount(ctx)

	order := &models.PaymentOrder{
		AccountID: acc.ID,
		AmountRub: 250.0,
		Status:    "pending",
		Metadata:  map[string]interface{}{"key": "value"},
	}
	err := repo.CreatePaymentOrder(ctx, order)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.ID == 0 {
		t.Fatal("expected order id")
	}

	// Get by ID
	found, err := repo.GetPaymentOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != order.ID {
		t.Fatalf("expected id %d, got %d", order.ID, found.ID)
	}

	// Update
	found.Status = "paid"
	now := time.Now()
	found.PaidAt = &now
	found.YookassaPaymentID = strPtr("pay_123")
	err = repo.UpdatePaymentOrder(ctx, found)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get by YookassaID
	byYookassa, err := repo.GetPaymentOrderByYookassaID(ctx, "pay_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if byYookassa.Status != "paid" {
		t.Fatalf("expected status paid, got %s", byYookassa.Status)
	}
}

func TestPostgresRepository_GetExpiredPendingPayments(t *testing.T) {
	repo, pool := setupBillingRepo(t)
	ctx := context.Background()

	acc, _ := repo.CreateAccount(ctx)
	pool.Exec(ctx,
		`INSERT INTO payment_orders (account_id, amount_rub, status, created_at) VALUES ($1, 100.00, 'pending', NOW() - INTERVAL '2 hours')`,
		acc.ID)

	orders, err := repo.GetExpiredPendingPayments(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected 1 expired order, got %d", len(orders))
	}
}

func TestPostgresRepository_BeginTx(t *testing.T) {
	repo, _ := setupBillingRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create account in tx
	acc, _ := repo.WithTx(tx).CreateAccount(ctx)
	if acc.ID == 0 {
		t.Fatal("expected account id")
	}

	// Rollback
	tx.Rollback(ctx)

	// Account should not exist after rollback
	_, err = repo.GetAccount(ctx, acc.ID)
	if err == nil {
		t.Fatal("expected error for rolled back account")
	}
}

func strPtr(s string) *string {
	return &s
}
