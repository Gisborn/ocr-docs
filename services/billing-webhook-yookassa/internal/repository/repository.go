package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/api-scan/api-scan/pkg/billing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository интерфейс для работы с billing данными
type Repository interface {
	GetPaymentOrderByYookassaID(ctx context.Context, paymentID string) (*billing.PaymentOrder, error)
	UpdatePaymentOrder(ctx context.Context, order *billing.PaymentOrder) error
	CreateBillingEvent(ctx context.Context, event *billing.BillingEvent) error
}

// PostgresRepository реализация на PostgreSQL
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository создает новый репозиторий
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// GetPaymentOrderByYookassaID возвращает заказ по ID платежа ЮКассы
func (r *PostgresRepository) GetPaymentOrderByYookassaID(ctx context.Context, paymentID string) (*billing.PaymentOrder, error) {
	order := &billing.PaymentOrder{}
	var metadataBytes []byte
	
	err := r.pool.QueryRow(ctx,
		`SELECT id, account_id, amount_rub, status, yookassa_payment_id, metadata, created_at, paid_at
		 FROM payment_orders WHERE yookassa_payment_id = $1`,
		paymentID,
	).Scan(&order.ID, &order.AccountID, &order.AmountRub, &order.Status,
		&order.YookassaPaymentID, &metadataBytes, &order.CreatedAt, &order.PaidAt)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get payment order: %w", err)
	}
	
	order.Metadata = make(map[string]interface{})
	return order, nil
}

// UpdatePaymentOrder обновляет заказ
func (r *PostgresRepository) UpdatePaymentOrder(ctx context.Context, order *billing.PaymentOrder) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE payment_orders 
		 SET status = $1, yookassa_payment_id = $2, paid_at = $3
		 WHERE id = $4`,
		order.Status, order.YookassaPaymentID, order.PaidAt, order.ID,
	)
	if err != nil {
		return fmt.Errorf("update payment order: %w", err)
	}
	return nil
}

// CreateBillingEvent создает событие биллинга
func (r *PostgresRepository) CreateBillingEvent(ctx context.Context, event *billing.BillingEvent) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO billing_events (account_id, subscription_id, type, service_id, request_id, real_amount_rub, prepaid_amount_rub, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		event.AccountID, event.SubscriptionID, event.Type, event.ServiceID, event.RequestID,
		event.RealAmountRub, event.PrepaidAmountRub,
	)
	if err != nil {
		return fmt.Errorf("create billing event: %w", err)
	}
	return nil
}

// GetExpiredPendingPayments возвращает просроченные pending платежи
func (r *PostgresRepository) GetExpiredPendingPayments(ctx context.Context, timeout time.Duration) ([]*billing.PaymentOrder, error) {
	cutoff := time.Now().Add(-timeout)
	
	rows, err := r.pool.Query(ctx,
		`SELECT id, account_id, amount_rub, status, yookassa_payment_id, metadata, created_at, paid_at
		 FROM payment_orders 
		 WHERE status = 'pending' AND created_at < $1`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("query expired payments: %w", err)
	}
	defer rows.Close()

	var orders []*billing.PaymentOrder
	for rows.Next() {
		order := &billing.PaymentOrder{}
		var metadataBytes []byte
		
		err := rows.Scan(&order.ID, &order.AccountID, &order.AmountRub, &order.Status,
			&order.YookassaPaymentID, &metadataBytes, &order.CreatedAt, &order.PaidAt)
		if err != nil {
			continue
		}
		
		order.Metadata = make(map[string]interface{})
		orders = append(orders, order)
	}
	
	return orders, rows.Err()
}
