package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"scan.passport.local/api/pkg/billing"
	"scan.passport.local/api/pkg/testdb"
)

func setupWebhookRepo(t *testing.T) (*PostgresRepository, *pgxpool.Pool) {
	pool := testdb.SetupTestDB(t, testdb.DefaultBillingURL(), "../../../../migrations/billing")
	return NewPostgresRepository(pool), pool
}

func createTestAccount(ctx context.Context, pool *pgxpool.Pool) int64 {
	var id int64
	err := pool.QueryRow(ctx, `INSERT INTO accounts (status) VALUES ('active') RETURNING id`).Scan(&id)
	if err != nil {
		panic(err)
	}
	return id
}

func TestPostgresRepository_GetPaymentOrderByYookassaID(t *testing.T) {
	repo, pool := setupWebhookRepo(t)
	ctx := context.Background()

	accountID := createTestAccount(ctx, pool)
	yookassaID := "pay_12345"

	var orderID int32
	err := pool.QueryRow(ctx,
		`INSERT INTO payment_orders (account_id, amount_rub, status, yookassa_payment_id) VALUES ($1, 100.00, 'pending', $2) RETURNING id`,
		accountID, yookassaID).Scan(&orderID)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}

	order, err := repo.GetPaymentOrderByYookassaID(ctx, yookassaID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order == nil {
		t.Fatal("expected order")
	}
	if order.ID != orderID {
		t.Fatalf("expected id %d, got %d", orderID, order.ID)
	}
	if order.AccountID != accountID {
		t.Fatalf("expected account_id %d, got %d", accountID, order.AccountID)
	}
	if order.Status != "pending" {
		t.Fatalf("expected status pending, got %s", order.Status)
	}

	// Not found
	order, err = repo.GetPaymentOrderByYookassaID(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order != nil {
		t.Fatal("expected nil")
	}
}

func TestPostgresRepository_UpdatePaymentOrder(t *testing.T) {
	repo, pool := setupWebhookRepo(t)
	ctx := context.Background()

	accountID := createTestAccount(ctx, pool)
	var orderID int32
	pool.QueryRow(ctx,
		`INSERT INTO payment_orders (account_id, amount_rub, status, yookassa_payment_id) VALUES ($1, 200.00, 'pending', 'pay_2') RETURNING id`,
		accountID).Scan(&orderID)

	now := time.Now()
	err := repo.UpdatePaymentOrder(ctx, &billing.PaymentOrder{
		ID:                orderID,
		Status:            "paid",
		YookassaPaymentID: strPtr("pay_2"),
		PaidAt:            &now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status string
	pool.QueryRow(ctx, `SELECT status FROM payment_orders WHERE id = $1`, orderID).Scan(&status)
	if status != "paid" {
		t.Fatalf("expected status paid, got %s", status)
	}
}

func TestPostgresRepository_CreateBillingEvent(t *testing.T) {
	repo, pool := setupWebhookRepo(t)
	ctx := context.Background()

	accountID := createTestAccount(ctx, pool)
	event := &billing.BillingEvent{
		AccountID:        accountID,
		Type:             "balance_topup",
		RealAmountRub:    50.0,
		PrepaidAmountRub: 0,
	}

	err := repo.CreateBillingEvent(ctx, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM billing_events WHERE account_id = $1`, accountID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}

func TestPostgresRepository_GetExpiredPendingPayments(t *testing.T) {
	repo, pool := setupWebhookRepo(t)
	ctx := context.Background()

	accountID := createTestAccount(ctx, pool)

	// Insert expired pending payment
	pool.Exec(ctx,
		`INSERT INTO payment_orders (account_id, amount_rub, status, yookassa_payment_id, created_at) VALUES ($1, 100.00, 'pending', 'pay_old', NOW() - INTERVAL '2 hours')`,
		accountID)

	// Insert recent pending payment
	pool.Exec(ctx,
		`INSERT INTO payment_orders (account_id, amount_rub, status, yookassa_payment_id, created_at) VALUES ($1, 200.00, 'pending', 'pay_new', NOW())`,
		accountID)

	orders, err := repo.GetExpiredPendingPayments(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected 1 expired order, got %d", len(orders))
	}
	if orders[0].AmountRub != 100.0 {
		t.Fatalf("expected amount 100, got %f", orders[0].AmountRub)
	}
}

func strPtr(s string) *string {
	return &s
}
