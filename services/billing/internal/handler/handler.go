package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/api-scan/api-scan/services/billing/internal/service"
)

// Handler HTTP обработчики Billing Service
type Handler struct {
	billingService  *service.BillingService
	subService      *service.SubscriptionService
	paymentService  *service.PaymentService
	yookassaSecret  string
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

// Health проверка здоровья
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// CreateAccount создает новый счет
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

// GetBalance возвращает баланс счета
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

// Reserve резервирует средства
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
		switch err {
		case service.ErrAccountBlocked:
			http.Error(w, "Account is blocked", http.StatusForbidden)
		case service.ErrAccountArchived:
			http.Error(w, "Account not found", http.StatusNotFound)
		case service.ErrInsufficientBalance:
			http.Error(w, "Insufficient balance", http.StatusPaymentRequired)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Commit фиксирует списание
func (h *Handler) Commit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	transactionID := extractTransactionID(r.URL.Path)
	if transactionID == "" {
		http.Error(w, "Invalid transaction ID", http.StatusBadRequest)
		return
	}

	if err := h.billingService.Commit(r.Context(), transactionID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "committed"})
}

// Rollback откатывает списание
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	transactionID := extractTransactionID(r.URL.Path)
	if transactionID == "" {
		http.Error(w, "Invalid transaction ID", http.StatusBadRequest)
		return
	}

	body, _ := io.ReadAll(r.Body)
	var req struct {
		Reason string `json:"reason"`
	}
	json.Unmarshal(body, &req)

	if err := h.billingService.Rollback(r.Context(), transactionID, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "rolled_back"})
}

// CreateSubscription создает подписку
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
			http.Error(w, "Insufficient balance", http.StatusPaymentRequired)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// Upgrade выполняет апгрейд подписки
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

	var req service.UpgradeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	resp, err := h.subService.Upgrade(r.Context(), accountID, &req)
	if err != nil {
		if err == service.ErrInsufficientBalance {
			http.Error(w, "Insufficient balance", http.StatusPaymentRequired)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// CreatePayment создает новый платеж (пополнение баланса)
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

// GetPayment возвращает информацию о платеже
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
