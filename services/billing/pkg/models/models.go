package models

import (
	"time"
)

// Account счет клиента
type Account struct {
	ID        int64     `json:"id" db:"id"`
	Status    string    `json:"status" db:"status"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Service тип услуги
type Service struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Status      string    `json:"status" db:"status"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Tariff справочник тарифа
type Tariff struct {
	ID          int16     `json:"id" db:"id"`
	Code        string    `json:"code" db:"code"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	IsActive    bool      `json:"is_active" db:"is_active"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// TariffVersion версия тарифа с ценами
type TariffVersion struct {
	ID               int32     `json:"id" db:"id"`
	TariffID         int16     `json:"tariff_id" db:"tariff_id"`
	ValidFrom        time.Time `json:"valid_from" db:"valid_from"`
	ValidUntil       *time.Time `json:"valid_until,omitempty" db:"valid_until"`
	DurationDays     int       `json:"duration_days" db:"duration_days"`
	BasePriceRub     float64   `json:"base_price_rub" db:"base_price_rub"`
	PrepaidAmountRub float64   `json:"prepaid_amount_rub" db:"prepaid_amount_rub"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// TariffWithVersion тариф с текущей активной версией
type TariffWithVersion struct {
	ID               int16     `json:"id"`
	Code             string    `json:"code"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	BasePriceRub     float64   `json:"base_price_rub"`
	PrepaidAmountRub float64   `json:"prepaid_amount_rub"`
	DurationDays     int       `json:"duration_days"`
}

// TariffServicePrice цена услуги в тарифе
type TariffServicePrice struct {
	ID                int32     `json:"id" db:"id"`
	TariffVersionID   int32     `json:"tariff_version_id" db:"tariff_version_id"`
	ServiceID         string    `json:"service_id" db:"service_id"`
	IncludedPriceRub  float64   `json:"included_price_rub" db:"included_price_rub"`
	OveragePriceRub   float64   `json:"overage_price_rub" db:"overage_price_rub"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// Subscription подписка
type Subscription struct {
	ID                 int32      `json:"id" db:"id"`
	AccountID          int64      `json:"account_id" db:"account_id"`
	TariffVersionID    int32      `json:"tariff_version_id" db:"tariff_version_id"`
	Status             string     `json:"status" db:"status"`
	StartedAt          time.Time  `json:"started_at" db:"started_at"`
	ExpiresAt          time.Time  `json:"expires_at" db:"expires_at"`
	GracePeriodEndsAt  *time.Time `json:"grace_period_ends_at,omitempty" db:"grace_period_ends_at"`
	InitialPrepaidRub  float64    `json:"initial_prepaid_rub" db:"initial_prepaid_rub"`
	AutoRenew          bool       `json:"auto_renew" db:"auto_renew"`
	NextTariffVersionID *int32    `json:"next_tariff_version_id,omitempty" db:"next_tariff_version_id"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

// BillingEvent событие биллинга (event sourcing)
type BillingEvent struct {
	ID              int64     `json:"id" db:"id"`
	AccountID       int64     `json:"account_id" db:"account_id"`
	SubscriptionID  *int32    `json:"subscription_id,omitempty" db:"subscription_id"`
	ServiceID       *string   `json:"service_id,omitempty" db:"service_id"`
	Type            string    `json:"type" db:"type"`
	RealAmountRub   float64   `json:"real_amount_rub" db:"real_amount_rub"`
	PrepaidAmountRub float64  `json:"prepaid_amount_rub" db:"prepaid_amount_rub"`
	RequestID       *string   `json:"request_id,omitempty" db:"request_id"`
	Metadata        map[string]interface{} `json:"metadata" db:"metadata"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

// BalanceSnapshot снапшот баланса
type BalanceSnapshot struct {
	AccountID         int64     `json:"account_id" db:"account_id"`
	RealBalanceRub    float64   `json:"real_balance_rub" db:"real_balance_rub"`
	PrepaidBalanceRub float64   `json:"prepaid_balance_rub" db:"prepaid_balance_rub"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// Reservation активный резерв
type Reservation struct {
	ID              int64     `json:"id" db:"id"`
	AccountID       int64     `json:"account_id" db:"account_id"`
	SubscriptionID  *int32    `json:"subscription_id,omitempty" db:"subscription_id"`
	ServiceID       *string   `json:"service_id,omitempty" db:"service_id"`
	RequestID       string    `json:"request_id" db:"request_id"`
	AmountRub       float64   `json:"amount_rub" db:"amount_rub"`
	ChargeType      string    `json:"charge_type" db:"charge_type"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	ExpiresAt       time.Time `json:"expires_at" db:"expires_at"`
}

// PaymentOrder заказ на оплату
type PaymentOrder struct {
	ID                int32     `json:"id" db:"id"`
	AccountID         int64     `json:"account_id" db:"account_id"`
	AmountRub         float64   `json:"amount_rub" db:"amount_rub"`
	Status            string    `json:"status" db:"status"`
	YookassaPaymentID *string   `json:"yookassa_payment_id,omitempty" db:"yookassa_payment_id"`
	Metadata          map[string]interface{} `json:"metadata" db:"metadata"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	PaidAt            *time.Time `json:"paid_at,omitempty" db:"paid_at"`
}

// Balance с балансом для ответа API
type Balance struct {
	AccountID         int64                  `json:"account_id"`
	RealBalanceRub    float64                `json:"real_balance_rub"`
	PrepaidBalanceRub float64                `json:"prepaid_balance_rub"`
	ActiveSubscription *SubscriptionInfo     `json:"active_subscription,omitempty"`
	CalculatedAt      time.Time              `json:"calculated_at"`
}

// SubscriptionInfo информация о подписке для ответа
type SubscriptionInfo struct {
	ID              int32   `json:"id"`
	TariffCode      string  `json:"tariff_code"`
	TariffName      string  `json:"tariff_name"`
	ExpiresAt       time.Time `json:"expires_at"`
	Status          string  `json:"status"`
	PrepaidRemaining float64 `json:"prepaid_remaining_rub"`
}
