package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"scan.passport.local/api/services/cabinet/internal/middleware"
	"scan.passport.local/api/services/cabinet/internal/service"
)

// Handler HTTP обработчики Cabinet Service
type Handler struct {
	authService    *service.AuthService
	apiKeyService  *service.APIKeyService
	paymentService *service.PaymentService
	subService     *service.SubscriptionService
}

// NewHandler создает новый handler
func NewHandler(auth *service.AuthService, apiKey *service.APIKeyService, payment *service.PaymentService, sub *service.SubscriptionService) *Handler {
	return &Handler{
		authService:    auth,
		apiKeyService:  apiKey,
		paymentService: payment,
		subService:     sub,
	}
}

// Health godoc
// @Summary Health check
// @Description Check if the cabinet service is running
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health [get]
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// Register godoc
// @Summary Register new organization
// @Description Register a new organization with admin user
// @Tags auth
// @Accept json
// @Produce json
// @Param request body service.RegisterRequest true "Registration data"
// @Success 201 {object} service.RegisterResponse
// @Failure 400 {object} map[string]string
// @Router /api/v1/auth/register [post]
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req service.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.authService.Register(r.Context(), &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// Login godoc
// @Summary Login
// @Description Authenticate and get session token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body service.LoginRequest true "Login credentials"
// @Success 200 {object} service.LoginResponse
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req service.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.authService.Login(r.Context(), &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		return
	}

	// Устанавливаем cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    resp.SessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Verify godoc
// @Summary Verify session
// @Description Verify session token and return user info
// @Tags auth
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/verify [get]
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	userID := middleware.GetUserID(r.Context())

	if orgID == 0 || userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Get org details (fresh from DB, not cached)
	org, err := h.authService.GetOrgByID(r.Context(), orgID)
	if err != nil {
		http.Error(w, `{"error":"organization not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"org_id":             orgID,
		"user_id":            userID,
		"billing_account_id": org.BillingAccountID,
		"email":              org.Email,
		"org_name":           org.Name,
	})
}

// Logout godoc
// @Summary Logout
// @Description Invalidate session
// @Tags auth
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Получаем токен
	token := extractToken(r)
	if token != "" {
		_ = h.authService.Logout(r.Context(), token)
	}

	// Удаляем cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "logged out"})
}

// CreateAPIKey godoc
// @Summary Create API key
// @Description Create a new API key for the organization
// @Tags api-keys
// @Accept json
// @Produce json
// @Param request body service.CreateAPIKeyRequest true "API key name"
// @Success 201 {object} service.CreateAPIKeyResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/v1/api-keys [post]
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	userID := middleware.GetUserID(r.Context())

	if orgID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req service.CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.apiKeyService.CreateAPIKey(r.Context(), orgID, userID, &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// ListAPIKeys godoc
// @Summary List API keys
// @Description Get list of API keys for the organization
// @Tags api-keys
// @Accept json
// @Produce json
// @Success 200 {array} service.APIKeyInfo
// @Failure 401 {object} map[string]string
// @Router /api/v1/api-keys [get]
func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	keys, err := h.apiKeyService.ListAPIKeys(r.Context(), orgID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// RevokeAPIKey godoc
// @Summary Revoke API key
// @Description Revoke an API key
// @Tags api-keys
// @Accept json
// @Produce json
// @Param id path int true "API Key ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/v1/api-keys/{id} [delete]
func (h *Handler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	userID := middleware.GetUserID(r.Context())

	if orgID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Извлекаем ID из пути
	keyID, err := extractIDFromPath(r.URL.Path, "/api/v1/api-keys/")
	if err != nil {
		http.Error(w, `{"error":"invalid key id"}`, http.StatusBadRequest)
		return
	}

	if err := h.apiKeyService.RevokeAPIKey(r.Context(), orgID, keyID, userID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

// CreateMockPayment создает мок-платеж для пополнения баланса
func (h *Handler) CreateMockPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	accountID := middleware.GetBillingAccountID(r.Context())

	if orgID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req service.MockPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if req.AmountRub <= 0 {
		http.Error(w, `{"error":"amount must be positive"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.paymentService.CreateMockPayment(r.Context(), orgID, accountID, &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// ConfirmMockPayment подтверждает мок-платеж (симуляция webhook)
func (h *Handler) ConfirmMockPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Извлекаем payment_id из пути
	paymentID := extractPaymentIDFromPath(r.URL.Path)
	if paymentID == "" {
		http.Error(w, `{"error":"invalid payment id"}`, http.StatusBadRequest)
		return
	}

	err := h.paymentService.ConfirmMockPayment(r.Context(), paymentID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":      "completed",
		"payment_id":  paymentID,
		"message":     "Платеж успешно подтвержден. Баланс пополнен.",
	})
}

// Вспомогательные функции

func extractToken(r *http.Request) string {
	// Пробуем заголовок Authorization
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Пробуем cookie
	if cookie, err := r.Cookie("session"); err == nil {
		return cookie.Value
	}

	// Пробуем query параметр (для разработки)
	return r.URL.Query().Get("token")
}

func extractIDFromPath(path, prefix string) (int64, error) {
	idx := strings.Index(path, prefix)
	if idx == -1 {
		return 0, strconv.ErrRange
	}
	idStr := path[idx+len(prefix):]
	// Убираем trailing slash
	idStr = strings.TrimSuffix(idStr, "/")
	return strconv.ParseInt(idStr, 10, 64)
}

func extractPaymentIDFromPath(path string) string {
	// Путь вида /api/v1/payments/mock_123456_1/confirm
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "mock_") && i < len(parts)-1 && parts[i+1] == "confirm" {
			return part
		}
	}
	// Пробуем другой вариант: /api/v1/payments/mock_123456/confirm
	for i, part := range parts {
		if i > 0 && parts[i-1] == "payments" && strings.HasPrefix(part, "mock_") {
			return part
		}
	}
	return ""
}

// GetBalance возвращает баланс организации (через Billing Service)
func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	accountID := middleware.GetBillingAccountID(r.Context())
	if accountID == 0 {
		http.Error(w, `{"error":"billing account not found"}`, http.StatusBadRequest)
		return
	}

	balance, err := h.paymentService.GetBalance(r.Context(), accountID)
	if err != nil {
		http.Error(w, `{"error":"failed to get balance"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(balance)
}

// GetSubscription возвращает активную подписку организации
func (h *Handler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	accountID := middleware.GetBillingAccountID(r.Context())
	if accountID == 0 {
		http.Error(w, `{"error":"billing account not found"}`, http.StatusBadRequest)
		return
	}

	sub, err := h.subService.GetSubscription(r.Context(), accountID)
	if err != nil {
		if err.Error() == "no active subscription" {
			http.Error(w, `{"error":"no active subscription"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to get subscription"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

// CreateSubscription создает подписку для организации
func (h *Handler) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	accountID := middleware.GetBillingAccountID(r.Context())
	if accountID == 0 {
		http.Error(w, `{"error":"billing account not found"}`, http.StatusBadRequest)
		return
	}

	var req service.CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	sub, err := h.subService.CreateSubscription(r.Context(), accountID, &req)
	if err != nil {
		if err.Error() == "insufficient balance" {
			http.Error(w, `{"error":"insufficient balance"}`, http.StatusPaymentRequired)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sub)
}

// GetHistory возвращает историю биллинг-событий организации
func (h *Handler) GetHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	accountID := middleware.GetBillingAccountID(r.Context())
	if accountID == 0 {
		http.Error(w, `{"error":"billing account not found"}`, http.StatusBadRequest)
		return
	}

	events, err := h.paymentService.GetBillingEvents(r.Context(), accountID)
	if err != nil {
		http.Error(w, `{"error":"failed to get history"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}
