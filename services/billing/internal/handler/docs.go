package handler

// ErrorResponse represents an error response
// @Description Error response with code and message
type ErrorResponse struct {
	Error string `json:"error" example:"insufficient balance"`
	Code  string `json:"code" example:"PAYMENT_REQUIRED"`
}

// SuccessResponse represents a generic success response
// @Description Success response
type SuccessResponse struct {
	Status string `json:"status" example:"ok"`
}

// BalanceResponse represents account balance
// @Description Account balance with real and prepaid amounts
type BalanceResponse struct {
	AccountID         int64   `json:"account_id" example:"123"`
	RealBalanceRub    float64 `json:"real_balance_rub" example:"15000.50"`
	PrepaidBalanceRub float64 `json:"prepaid_balance_rub" example:"5000.00"`
}

// ReserveRequest represents a reservation request
// @Description Request to reserve funds for an operation
type ReserveRequest struct {
	ServiceID string `json:"service_id" example:"passport_rf"`
	RequestID string `json:"request_id" example:"req_abc123"`
}

// ReserveResponse represents a reservation response
// @Description Reservation result
type ReserveResponse struct {
	Reserved      bool    `json:"reserved" example:"true"`
	TransactionID string  `json:"transaction_id" example:"req_abc123"`
	ChargeType    string  `json:"charge_type" example:"prepaid"`
	AmountRub     float64 `json:"amount_rub" example:"7.00"`
}

// CreatePaymentRequest represents a payment creation request
// @Description Request to create a payment order
type CreatePaymentRequest struct {
	AmountRub   float64 `json:"amount_rub" example:"1000.00"`
	Description string  `json:"description" example:"Balance top-up"`
	ReturnURL   string  `json:"return_url" example:"https://example.com/payment/success"`
}

// CreatePaymentResponse represents a payment creation response
// @Description Payment order created successfully
type CreatePaymentResponse struct {
	OrderID    int32   `json:"order_id" example:"42"`
	PaymentURL string  `json:"payment_url" example:"https://yookassa.ru/payments/mock_42_123456"`
	Status     string  `json:"status" example:"pending"`
	AmountRub  float64 `json:"amount_rub" example:"1000.00"`
}

// CreateSubscriptionRequest represents a subscription creation request
// @Description Request to create a new subscription
type CreateSubscriptionRequest struct {
	TariffCode    string `json:"tariff_code" example:"pro"`
	PaymentMethod string `json:"payment_method" example:"balance"`
}

// CreateSubscriptionResponse represents a subscription creation response
// @Description Subscription created successfully
type CreateSubscriptionResponse struct {
	ID               int32   `json:"id" example:"1"`
	TariffCode       string  `json:"tariff_code" example:"pro"`
	Status           string  `json:"status" example:"active"`
	StartedAt        string  `json:"started_at" example:"2026-03-18T10:00:00Z"`
	ExpiresAt        string  `json:"expires_at" example:"2026-04-18T10:00:00Z"`
	PrepaidRemaining float64 `json:"prepaid_remaining_rub" example:"6000.00"`
}
