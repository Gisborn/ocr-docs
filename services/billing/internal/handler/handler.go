package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"scan.passport.local/api/services/billing/internal/service"
)

// Handler HTTP обработчики Billing Service
type Handler struct {
	billingService *service.BillingService
	subService     *service.SubscriptionService
	paymentService *service.PaymentService
	yookassaSecret string
}

// NewHandler создает новый handler
func NewHandler(billing *service.BillingService, subs *service.SubscriptionService, payment *service.PaymentService, yookassaSecret string) *Handler {
	return &Handler{
		billingService: billing,
		subService:     subs,
		paymentService: payment,
		yookassaSecret: yookassaSecret,
	}
}

// Health godoc
// @Summary Health check
// @Description Check if the billing service is running
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse
// @Router /health [get]
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GetTariffs godoc
// @Summary Get available tariffs
// @Description Returns all active tariffs with current pricing
// @Tags tariffs
// @Accept json
// @Produce json
// @Success 200 {array} models.TariffWithVersion
// @Router /tariffs [get]
func (h *Handler) GetTariffs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tariffs, err := h.subService.GetTariffs(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tariffs)
}

// CreateAccount godoc
// @Summary Create new account
// @Description Create a new billing account
// @Tags accounts
// @Accept json
// @Produce json
// @Success 201 {object} map[string]interface{} "Created account"
// @Failure 500 {object} ErrorResponse
// @Router /accounts [post]
func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	account, err := h.billingService.CreateAccount(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(account)
}

// GetBalance godoc
// @Summary Get account balance
// @Description Get real and prepaid balance for an account
// @Tags accounts
// @Accept json
// @Produce json
// @Param id path int true "Account ID"
// @Success 200 {object} BalanceResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /accounts/{id}/balance [get]
func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID, err := extractAccountID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	balance, err := h.subService.GetBalance(r.Context(), accountID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(balance)
}

// Reserve godoc
// @Summary Reserve funds
// @Description Reserve funds for an operation (two-phase commit)
// @Tags transactions
// @Accept json
// @Produce json
// @Param id path int true "Account ID"
// @Param request body ReserveRequest true "Reservation request"
// @Success 200 {object} ReserveResponse
// @Failure 400 {object} ErrorResponse
// @Failure 402 {object} ErrorResponse "Insufficient balance"
// @Failure 500 {object} ErrorResponse
// @Router /accounts/{id}/reserve [post]
func (h *Handler) Reserve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID, err := extractAccountID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var req service.ReserveRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	resp, err := h.billingService.Reserve(r.Context(), accountID, &req)
	if err != nil {
		if err == service.ErrInsufficientBalance {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "insufficient balance",
				Code:  "PAYMENT_REQUIRED",
			})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Commit godoc
// @Summary Commit transaction
// @Description Commit a reserved transaction (two-phase commit)
// @Tags transactions
// @Accept json
// @Produce json
// @Param id path string true "Transaction ID (request_id)"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /transactions/{id}/commit [post]
func (h *Handler) Commit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID := extractTransactionID(r.URL.Path)
	if requestID == "" {
		http.Error(w, "Invalid transaction ID", http.StatusBadRequest)
		return
	}

	if err := h.billingService.Commit(r.Context(), requestID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SuccessResponse{Status: "committed"})
}

// Rollback godoc
// @Summary Rollback transaction
// @Description Rollback a reserved transaction (two-phase commit)
// @Tags transactions
// @Accept json
// @Produce json
// @Param id path string true "Transaction ID (request_id)"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /transactions/{id}/rollback [post]
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID := extractTransactionID(r.URL.Path)
	if requestID == "" {
		http.Error(w, "Invalid transaction ID", http.StatusBadRequest)
		return
	}

	if err := h.billingService.Rollback(r.Context(), requestID, "client request"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SuccessResponse{Status: "rolled back"})
}

// TopupBalance пополняет баланс (для мок-платежей)
func (h *Handler) TopupBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID, err := extractAccountID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	var req struct {
		AmountRub float64 `json:"amount_rub"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.AmountRub <= 0 {
		http.Error(w, "Amount must be positive", http.StatusBadRequest)
		return
	}

	// Создаем billing event для пополнения
	if err := h.subService.CreateTopupEvent(r.Context(), accountID, req.AmountRub); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "success",
		"amount_rub":  req.AmountRub,
		"message":     "Баланс успешно пополнен",
	})
}

// GetAccountSubscription godoc
// @Summary Get active subscription
// @Description Get active subscription for an account
// @Tags subscriptions
// @Accept json
// @Produce json
// @Param id path int true "Account ID"
// @Success 200 {object} service.GetActiveSubscriptionResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /accounts/{id}/subscriptions [get]
func (h *Handler) GetAccountSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID, err := extractAccountID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	sub, err := h.subService.GetActiveSubscription(r.Context(), accountID)
	if err != nil {
		http.Error(w, `{"error":"no active subscription"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

// GetBillingEvents godoc
// @Summary Get billing events history
// @Description Get all billing events for an account
// @Tags accounts
// @Accept json
// @Produce json
// @Param id path int true "Account ID"
// @Success 200 {array} models.BillingEvent
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /accounts/{id}/events [get]
func (h *Handler) GetBillingEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID, err := extractAccountID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	events, err := h.subService.GetBillingEvents(r.Context(), accountID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// CreateSubscription godoc
// @Summary Create subscription
// @Description Create a new subscription for an account
// @Tags subscriptions
// @Accept json
// @Produce json
// @Param id path int true "Account ID"
// @Param request body CreateSubscriptionRequest true "Subscription request"
// @Success 201 {object} CreateSubscriptionResponse
// @Failure 400 {object} ErrorResponse
// @Failure 402 {object} ErrorResponse "Insufficient balance"
// @Failure 500 {object} ErrorResponse
// @Router /accounts/{id}/subscriptions [post]
func (h *Handler) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID, err := extractAccountID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var req service.CreateSubscriptionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	resp, err := h.subService.CreateSubscription(r.Context(), accountID, &req)
	if err != nil {
		if err == service.ErrInsufficientBalance {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "insufficient balance",
				Code:  "PAYMENT_REQUIRED",
			})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// Upgrade godoc
// @Summary Upgrade subscription
// @Description Upgrade subscription to a higher tier
// @Tags subscriptions
// @Accept json
// @Produce json
// @Param id path int true "Account ID"
// @Param request body map[string]string true "Upgrade request {\"tariff_code\":\"pro\",\"payment_method\":\"balance\"}"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 402 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /accounts/{id}/subscriptions/upgrade [post]
func (h *Handler) Upgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID, err := extractAccountID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var req service.UpgradeSubscriptionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	resp, err := h.subService.UpgradeSubscription(r.Context(), accountID, &req)
	if err != nil {
		if err == service.ErrInsufficientBalance {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "insufficient balance",
				Code:  "PAYMENT_REQUIRED",
			})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// CreatePayment godoc
// @Summary Create payment
// @Description Create a payment order for balance top-up
// @Tags payments
// @Accept json
// @Produce json
// @Param id path int true "Account ID"
// @Param request body CreatePaymentRequest true "Payment request"
// @Success 201 {object} CreatePaymentResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /accounts/{id}/payments [post]
func (h *Handler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accountID, err := extractAccountID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var req service.CreatePaymentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.AmountRub <= 0 {
		http.Error(w, "Invalid amount", http.StatusBadRequest)
		return
	}

	resp, err := h.paymentService.CreatePayment(r.Context(), accountID, &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// GetPayment godoc
// @Summary Get payment status
// @Description Get payment order details
// @Tags payments
// @Accept json
// @Produce json
// @Param id path int true "Payment Order ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /payments/{id} [get]
func (h *Handler) GetPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	orderID, err := extractPaymentID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid payment ID", http.StatusBadRequest)
		return
	}

	order, err := h.paymentService.GetPaymentOrder(r.Context(), orderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

// Вспомогательные функции

func extractAccountID(path string) (int64, error) {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "accounts" && i+1 < len(parts) {
			return strconv.ParseInt(parts[i+1], 10, 64)
		}
	}
	return 0, fmt.Errorf("account ID not found")
}

func extractTransactionID(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "transactions" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractPaymentID(path string) (int32, error) {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "payments" && i+1 < len(parts) {
			id, err := strconv.ParseInt(parts[i+1], 10, 32)
			return int32(id), err
		}
	}
	return 0, fmt.Errorf("payment ID not found")
}
