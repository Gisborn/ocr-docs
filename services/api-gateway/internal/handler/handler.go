package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"scan.passport.local/api/services/api-gateway/internal/middleware"
	"scan.passport.local/api/services/api-gateway/internal/repository"
)

// Handler HTTP handler для API Gateway
type Handler struct {
	routes       map[string]*url.URL
	client       *http.Client
	repo         repository.Repository
	billingToken string
}

// NewHandler создает новый handler
func NewHandler(orchestratorURL, billingURL, cabinetURL string) (*Handler, error) {
	routes := make(map[string]*url.URL)

	if orchestratorURL != "" {
		u, err := url.Parse(orchestratorURL)
		if err != nil {
			return nil, fmt.Errorf("invalid orchestrator URL: %w", err)
		}
		routes["orchestrator"] = u
	}

	if billingURL != "" {
		u, err := url.Parse(billingURL)
		if err != nil {
			return nil, fmt.Errorf("invalid billing URL: %w", err)
		}
		routes["billing"] = u
	}

	if cabinetURL != "" {
		u, err := url.Parse(cabinetURL)
		if err != nil {
			return nil, fmt.Errorf("invalid cabinet URL: %w", err)
		}
		routes["cabinet"] = u
	}

	return &Handler{
		routes: routes,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// SetRepository устанавливает репозиторий для resolveTarget с billing_account_id
func (h *Handler) SetRepository(repo repository.Repository) {
	h.repo = repo
}

// SetBillingToken устанавливает токен для внутренней аутентификации с Billing
func (h *Handler) SetBillingToken(token string) {
	h.billingToken = token
}

// Health godoc
// @Summary Health check
// @Description Check if the API Gateway is running
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"ok","service":"api-gateway"}`)
}

// ProxyHandler godoc
// @Summary Proxy request to downstream services
// @Description Proxy requests to Orchestrator, Billing, or other services based on path
// @Tags proxy
// @Accept json
// @Produce json
// @Param Authorization header string true "API Key (X-Api-Key)"
// @Param Idempotency-Key header string false "Idempotency key for recognize requests"
// @Success 200 {object} RecognizeResponse
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 404 {object} ErrorResponse "Not found"
// @Failure 429 {object} ErrorResponse "Rate limit exceeded"
// @Failure 503 {object} ErrorResponse "Service unavailable"
// @Router /v1/recognize [post]
// @Router /v1/billing/accounts/{id}/balance [get]
// @Router /v1/billing/accounts/{id}/reserve [post]
// @Router /v1/billing/transactions/{id}/commit [post]
// @Router /v1/billing/transactions/{id}/rollback [post]
func (h *Handler) ProxyHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Resolve /accounts/me/ → /accounts/{billing_account_id}/
	if strings.Contains(path, "/accounts/me/") {
		resolved, err := h.resolveMePath(r.Context(), path)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s","code":"NOT_FOUND"}`, err.Error()), http.StatusNotFound)
			return
		}
		path = resolved
	}

	// Определяем целевой сервис
	target, targetPath := h.resolveTarget(path)
	if target == nil {
		http.Error(w, `{"error":"not found","code":"NOT_FOUND"}`, http.StatusNotFound)
		return
	}

	// Создаем новый URL
	targetURL := *target
	targetURL.Path = targetPath
	targetURL.RawQuery = r.URL.RawQuery

	// Создаем новый запрос
	proxyReq, err := http.NewRequest(r.Method, targetURL.String(), r.Body)
	if err != nil {
		http.Error(w, `{"error":"internal error","code":"INTERNAL_ERROR"}`, http.StatusInternalServerError)
		return
	}

	// Копируем заголовки
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Передаем organization_id и api_key_id downstream сервисам
	if orgID := r.Context().Value(middleware.ContextKeyOrganizationID); orgID != nil {
		proxyReq.Header.Set("X-Organization-ID", fmt.Sprintf("%v", orgID))
	}
	if keyID := r.Context().Value(middleware.ContextKeyAPIKeyID); keyID != nil {
		proxyReq.Header.Set("X-API-Key-ID", fmt.Sprintf("%v", keyID))
	}

	// Передаем service token в Billing для internal аутентификации
	if h.billingToken != "" && target.Host == h.routes["billing"].Host {
		proxyReq.Header.Set("X-Service-Token", h.billingToken)
	}

	// Добавляем X-Forwarded-For
	if clientIP := r.Header.Get("X-Forwarded-For"); clientIP != "" {
		proxyReq.Header.Set("X-Forwarded-For", clientIP+", "+r.RemoteAddr)
	} else {
		proxyReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	}

	// Добавляем X-Request-ID если есть
	if requestID := r.Header.Get("X-Request-ID"); requestID == "" {
		proxyReq.Header.Set("X-Request-ID", generateRequestID())
	}

	// Выполняем запрос
	resp, err := h.client.Do(proxyReq)
	if err != nil {
		http.Error(w, `{"error":"service unavailable","code":"SERVICE_UNAVAILABLE"}`, http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	// Копируем заголовки ответа
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Устанавливаем статус
	w.WriteHeader(resp.StatusCode)

	// Копируем тело
	_, _ = io.Copy(w, resp.Body)
}

// resolveMePath заменяет /accounts/me/ на /accounts/{billing_account_id}/
func (h *Handler) resolveMePath(ctx context.Context, path string) (string, error) {
	if h.repo == nil {
		return "", fmt.Errorf("repository not configured")
	}

	orgID := ctx.Value(middleware.ContextKeyOrganizationID)
	if orgID == nil {
		return "", fmt.Errorf("organization not authenticated")
	}

	id, ok := orgID.(int64)
	if !ok {
		// Пробуем конвертировать из string
		if s, ok := orgID.(string); ok {
			var err error
			id, err = parseInt64(s)
			if err != nil {
				return "", fmt.Errorf("invalid organization id")
			}
		} else {
			return "", fmt.Errorf("invalid organization id type")
		}
	}

	org, err := h.repo.GetOrganization(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to get organization")
	}
	if org == nil {
		return "", fmt.Errorf("organization not found")
	}
	if org.BillingAccountID == nil || *org.BillingAccountID == 0 {
		return "", fmt.Errorf("billing account not configured")
	}

	return strings.Replace(path, "/accounts/me/", fmt.Sprintf("/accounts/%d/", *org.BillingAccountID), 1), nil
}

func parseInt64(s string) (int64, error) {
	var result int64
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// resolveTarget определяет целевой сервис и путь
func (h *Handler) resolveTarget(path string) (*url.URL, string) {
	// Маршрутизация по путям
	switch {
	// Orchestrator - распознавание
	case strings.HasPrefix(path, "/v1/recognize"):
		if u, ok := h.routes["orchestrator"]; ok {
			return u, path
		}

	// Cabinet: /accounts/me/ (но не /accounts/me/balance — это billing)
	case path == "/accounts/me" || path == "/accounts/me/":
		if u, ok := h.routes["cabinet"]; ok {
			return u, path
		}

	// Billing (поддерживаем и /v1/billing/ и /api/v1/billing/)
	case strings.HasPrefix(path, "/v1/billing/") || strings.HasPrefix(path, "/api/v1/billing/") || strings.HasPrefix(path, "/accounts/"):
		if u, ok := h.routes["billing"]; ok {
			// Преобразуем /api/v1/billing/accounts -> /accounts
			newPath := strings.TrimPrefix(path, "/api/v1/billing")
			if strings.HasPrefix(path, "/v1/billing/") {
				newPath = strings.TrimPrefix(path, "/v1/billing")
			}
			return u, newPath
		}

	// Cabinet (личный кабинет и auth)
	case strings.HasPrefix(path, "/api/v1/cabinet/") || strings.HasPrefix(path, "/api/v1/auth/") || strings.HasPrefix(path, "/api/v1/mock-payments") || strings.HasPrefix(path, "/api/v1/payments/") || strings.HasPrefix(path, "/api/v1/api-keys") || path == "/api/v1/balance":
		if u, ok := h.routes["cabinet"]; ok {
			// Для cabinet - /api/v1/cabinet/* -> /*
			// Для auth - /api/v1/auth/* -> /api/v1/auth/*
			// Для mock-payments - /api/v1/mock-payments/* -> /api/v1/mock-payments/*
			if strings.HasPrefix(path, "/api/v1/cabinet/") {
				return u, strings.TrimPrefix(path, "/api/v1/cabinet")
			}
			return u, path
		}

	// Webhooks - проксируем на billing-webhook-yookassa
	case strings.HasPrefix(path, "/webhooks/"):
		// Webhooks идут напрямую на webhook сервис
		if u, ok := h.routes["billing-webhook"]; ok {
			return u, path
		}
	}

	return nil, ""
}

// generateRequestID генерирует уникальный ID запроса
func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
