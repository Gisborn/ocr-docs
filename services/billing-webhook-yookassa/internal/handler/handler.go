package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"scan.passport.local/api/pkg/billing"
	"scan.passport.local/api/services/billing-webhook-yookassa/internal/repository"
)

// Handler HTTP обработчики Webhook Service
type Handler struct {
	repo         repository.Repository
	secretKey    string
	ipWhitelist  []string
}

// NewHandler создает новый handler
func NewHandler(repo repository.Repository, secretKey string, ipWhitelist string) *Handler {
	return &Handler{
		repo:        repo,
		secretKey:   secretKey,
		ipWhitelist: parseIPWhitelist(ipWhitelist),
	}
}

// Health проверка здоровья
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// YookassaWebhook обрабатывает вебхуки от ЮКассы
func (h *Handler) YookassaWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Проверка IP whitelist
	if !h.checkIP(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 2. Читаем тело
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// 3. Проверка HMAC подписи (если настроен секрет)
	if h.secretKey != "" {
		signature := r.Header.Get("X-Yookassa-Signature")
		if signature == "" {
			signature = r.Header.Get("X-Signature")
		}
		if !h.verifySignature(body, signature) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// 4. Парсим webhook
	var webhook struct {
		Type  string `json:"type"`
		Event string `json:"event"`
		Object struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Amount   struct {
				Value    string `json:"value"`
				Currency string `json:"currency"`
			} `json:"amount"`
			Metadata map[string]string `json:"metadata"`
		} `json:"object"`
	}

	if err := json.Unmarshal(body, &webhook); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 5. Проверяем тип
	if webhook.Type != "notification" {
		http.Error(w, "Invalid type", http.StatusBadRequest)
		return
	}

	// 6. Парсим сумму
	var amount float64
	if _, err := fmt.Sscanf(webhook.Object.Amount.Value, "%f", &amount); err != nil {
		amount = 0
	}

	// 7. Обрабатываем через сервис
	if err := h.processWebhook(r.Context(), webhook.Object.ID, webhook.Event, amount); err != nil {
		// Логируем ошибку, но всё равно возвращаем 200, чтобы ЮКасса не ретраила
		fmt.Printf("Error processing webhook: %v\n", err)
	}

	// 8. Возвращаем 200 OK (требование ЮКассы)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
}

// processWebhook обрабатывает webhook и обновляет заказ
func (h *Handler) processWebhook(ctx context.Context, paymentID string, eventType string, amount float64) error {
	// Ищем заказ по ID платежа ЮКассы
	order, err := h.repo.GetPaymentOrderByYookassaID(ctx, paymentID)
	if err != nil {
		return fmt.Errorf("order not found: %w", err)
	}
	if order == nil {
		return fmt.Errorf("order not found for payment_id: %s", paymentID)
	}

	// Проверяем, не обработан ли уже
	if order.Status == "completed" {
		return nil // Идемпотентность
	}

	// Если уже failed, но пришел success - восстанавливаем
	if order.Status == "failed" && eventType == "payment.succeeded" {
		order.Status = "pending" // Восстанавливаем для обработки
	}

	switch eventType {
	case "payment.succeeded":
		return h.processSuccess(ctx, order, amount)
	case "payment.canceled":
		return h.processCancel(ctx, order)
	default:
		return fmt.Errorf("unknown event type: %s", eventType)
	}
}

// processSuccess обрабатывает успешный платеж
func (h *Handler) processSuccess(ctx context.Context, order *billing.PaymentOrder, amount float64) error {
	now := time.Now()
	order.Status = "completed"
	order.PaidAt = &now

	// Создаем billing event для пополнения баланса
	event := &billing.BillingEvent{
		AccountID:     order.AccountID,
		Type:          "balance_topup",
		RealAmountRub: amount,
	}

	// Обновляем заказ и создаем событие
	if err := h.repo.UpdatePaymentOrder(ctx, order); err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	if err := h.repo.CreateBillingEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to create billing event: %w", err)
	}

	return nil
}

// processCancel обрабатывает отмененный платеж
func (h *Handler) processCancel(ctx context.Context, order *billing.PaymentOrder) error {
	order.Status = "failed"
	return h.repo.UpdatePaymentOrder(ctx, order)
}

// checkIP проверяет IP адрес клиента по whitelist
func (h *Handler) checkIP(r *http.Request) bool {
	if len(h.ipWhitelist) == 0 {
		return true // Если whitelist не настроен, пропускаем все
	}

	// Получаем IP из заголовков (учитывая прокси)
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	// Проверяем по CIDR
	clientIP := net.ParseIP(ip)
	if clientIP == nil {
		return false
	}

	for _, cidr := range h.ipWhitelist {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.Contains(clientIP) {
			return true
		}
	}

	return false
}

// verifySignature проверяет HMAC подпись
func (h *Handler) verifySignature(body []byte, signature string) bool {
	// ЮКасса использует HMAC-SHA256 с секретным ключом
	// Формат: hex(hmac_sha256(secret_key, body))
	
	mac := hmac.New(sha256.New, []byte(h.secretKey))
	mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// parseIPWhitelist парсит строку с CIDR в слайс
func parseIPWhitelist(whitelist string) []string {
	if whitelist == "" {
		return nil
	}
	
	var result []string
	for _, cidr := range strings.Split(whitelist, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr != "" {
			result = append(result, cidr)
		}
	}
	return result
}
