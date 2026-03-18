package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Handler HTTP handler для API Gateway
type Handler struct {
	routes map[string]*url.URL
	client *http.Client
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

// Health проверка здоровья
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"ok","service":"api-gateway"}`)
}

// ProxyHandler проксирует запросы на downstream сервисы
func (h *Handler) ProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Определяем целевой сервис
	target, path := h.resolveTarget(r.URL.Path)
	if target == nil {
		http.Error(w, `{"error":"not found","code":"NOT_FOUND"}`, http.StatusNotFound)
		return
	}

	// Создаем новый URL
	targetURL := *target
	targetURL.Path = path
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
	io.Copy(w, resp.Body)
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

	// Billing
	case strings.HasPrefix(path, "/v1/billing/") || strings.HasPrefix(path, "/accounts/"):
		if u, ok := h.routes["billing"]; ok {
			// Преобразуем /v1/billing/accounts -> /accounts
			newPath := strings.TrimPrefix(path, "/v1/billing")
			return u, newPath
		}

	// Cabinet (личный кабинет)
	case strings.HasPrefix(path, "/v1/cabinet/"):
		if u, ok := h.routes["cabinet"]; ok {
			newPath := strings.TrimPrefix(path, "/v1/cabinet")
			return u, newPath
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
