package billing

import (
	"context"
	"time"
)

// Repository интерфейс для работы с billing данными (shared между сервисами)
type Repository interface {
	// Payment Orders
	GetPaymentOrder(ctx context.Context, id int32) (*PaymentOrder, error)
	GetPaymentOrderByYookassaID(ctx context.Context, paymentID string) (*PaymentOrder, error)
	UpdatePaymentOrder(ctx context.Context, order *PaymentOrder) error
	
	// Billing Events
	CreateBillingEvent(ctx context.Context, event *BillingEvent) error
}

// PaymentOrder заказ на оплату
type PaymentOrder struct {
	ID                int32                  `json:"id" db:"id"`
	AccountID         int64                  `json:"account_id" db:"account_id"`
	AmountRub         float64                `json:"amount_rub" db:"amount_rub"`
	Status            string                 `json:"status" db:"status"`
	YookassaPaymentID *string                `json:"yookassa_payment_id,omitempty" db:"yookassa_payment_id"`
	Metadata          map[string]interface{} `json:"metadata" db:"metadata"`
	CreatedAt         time.Time              `json:"created_at" db:"created_at"`
	PaidAt            *time.Time             `json:"paid_at,omitempty" db:"paid_at"`
}

// BillingEvent событие биллинга
type BillingEvent struct {
	ID               int64      `json:"id" db:"id"`
	AccountID        int64      `json:"account_id" db:"account_id"`
	SubscriptionID   *int32     `json:"subscription_id,omitempty" db:"subscription_id"`
	Type             string     `json:"type" db:"type"`
	ServiceID        *string    `json:"service_id,omitempty" db:"service_id"`
	RequestID        *string    `json:"request_id,omitempty" db:"request_id"`
	RealAmountRub    float64    `json:"real_amount_rub" db:"real_amount_rub"`
	PrepaidAmountRub float64    `json:"prepaid_amount_rub" db:"prepaid_amount_rub"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
}
