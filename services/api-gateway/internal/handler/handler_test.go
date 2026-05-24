package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"scan.passport.local/api/services/api-gateway/internal/middleware"
)

func TestNewHandler_InvalidURL(t *testing.T) {
	_, err := NewHandler("://invalid", "", "")
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestHandler_Health(t *testing.T) {
	h, _ := NewHandler("", "", "")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "api-gateway") {
		t.Errorf("Expected api-gateway in response, got %s", rr.Body.String())
	}
}

func TestHandler_HealthMethodNotAllowed(t *testing.T) {
	h, _ := NewHandler("", "", "")

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rr.Code)
	}
}

func TestHandler_ProxyToOrchestrator(t *testing.T) {
	// Создаем мок orchestrator
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/recognize" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"request_id":"req_123","last_name":"Ivanov"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer orch.Close()

	h, _ := NewHandler(orch.URL, "", "")

	req := httptest.NewRequest(http.MethodPost, "/v1/recognize", nil)
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "req_123") {
		t.Errorf("Expected proxied response, got %s", rr.Body.String())
	}
}

func TestHandler_ProxyToBilling(t *testing.T) {
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/1/balance" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"real_balance_rub":1500}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billing.Close()

	h, _ := NewHandler("", billing.URL, "")

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/accounts/1/balance", nil)
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "1500") {
		t.Errorf("Expected balance, got %s", rr.Body.String())
	}
}

func TestHandler_ProxyToBillingWithApiPrefix(t *testing.T) {
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/2/reserve" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"reserved":true}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer billing.Close()

	h, _ := NewHandler("", billing.URL, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/accounts/2/reserve", nil)
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "reserved") {
		t.Errorf("Expected reserved response, got %s", rr.Body.String())
	}
}

func TestHandler_ProxyToCabinet(t *testing.T) {
	cabinet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"session_token":"tok_123"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cabinet.Close()

	h, _ := NewHandler("", "", cabinet.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "tok_123") {
		t.Errorf("Expected session token, got %s", rr.Body.String())
	}
}

func TestHandler_ProxyToCabinetSubpath(t *testing.T) {
	cabinet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/profile" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"name":"Test Org"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cabinet.Close()

	h, _ := NewHandler("", "", cabinet.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cabinet/profile", nil)
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Test Org") {
		t.Errorf("Expected profile, got %s", rr.Body.String())
	}
}

func TestHandler_ProxyNotFound(t *testing.T) {
	h, _ := NewHandler("", "", "")

	req := httptest.NewRequest(http.MethodGet, "/unknown/path", nil)
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rr.Code)
	}
}

func TestHandler_ProxyServiceUnavailable(t *testing.T) {
	// Создаем сервер который сразу закрывается
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	orch.Close()

	h, _ := NewHandler(orch.URL, "", "")

	req := httptest.NewRequest(http.MethodPost, "/v1/recognize", nil)
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", rr.Code)
	}
}

func TestHandler_ProxyHeaders(t *testing.T) {
	var capturedHeaders http.Header
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer orch.Close()

	h, _ := NewHandler(orch.URL, "", "")

	req := httptest.NewRequest(http.MethodPost, "/v1/recognize", nil)
	req.Header.Set("X-Api-Key", "test-key")
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Content-Type", "application/json")

	// Добавляем контекст с org_id и key_id (как после auth middleware)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyOrganizationID, int64(42))
	ctx = context.WithValue(ctx, middleware.ContextKeyAPIKeyID, int64(7))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.ProxyHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	// Проверяем что заголовки проброшены downstream
	if capturedHeaders.Get("X-Api-Key") != "test-key" {
		t.Errorf("Expected X-Api-Key forwarded")
	}
	if capturedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("Expected X-Custom-Header forwarded")
	}
	if capturedHeaders.Get("X-Organization-ID") != "42" {
		t.Errorf("Expected X-Organization-ID=42, got %s", capturedHeaders.Get("X-Organization-ID"))
	}
	if capturedHeaders.Get("X-API-Key-ID") != "7" {
		t.Errorf("Expected X-API-Key-ID=7, got %s", capturedHeaders.Get("X-API-Key-ID"))
	}
	if capturedHeaders.Get("X-Request-ID") == "" {
		t.Errorf("Expected X-Request-ID to be set")
	}
	if capturedHeaders.Get("X-Forwarded-For") == "" {
		t.Errorf("Expected X-Forwarded-For to be set")
	}
}

func TestHandler_ProxyBody(t *testing.T) {
	var capturedBody string
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer orch.Close()

	h, _ := NewHandler(orch.URL, "", "")

	body := `{"file":"data"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/recognize", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if capturedBody != body {
		t.Errorf("Expected body %q, got %q", body, capturedBody)
	}
}

func TestHandler_ProxyQueryParams(t *testing.T) {
	var capturedQuery string
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `[]`)
	}))
	defer billing.Close()

	h, _ := NewHandler("", billing.URL, "")

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/accounts/1/events?limit=10&offset=0", nil)
	rr := httptest.NewRecorder()

	h.ProxyHandler(rr, req)

	if capturedQuery != "limit=10&offset=0" {
		t.Errorf("Expected query params, got %q", capturedQuery)
	}
}

func TestResolveTarget(t *testing.T) {
	h, _ := NewHandler(
		"http://orchestrator:8080",
		"http://billing:8080",
		"http://cabinet:8080",
	)

	tests := []struct {
		path        string
		expectHost  string
		expectPath  string
	}{
		{"/v1/recognize", "http://orchestrator:8080", "/v1/recognize"},
		{"/v1/billing/accounts/1", "http://billing:8080", "/accounts/1"},
		{"/api/v1/billing/accounts/2", "http://billing:8080", "/accounts/2"},
		{"/accounts/3/balance", "http://billing:8080", "/accounts/3/balance"},
		{"/api/v1/auth/login", "http://cabinet:8080", "/api/v1/auth/login"},
		{"/api/v1/cabinet/profile", "http://cabinet:8080", "/profile"},
		{"/api/v1/api-keys", "http://cabinet:8080", "/api/v1/api-keys"},
		{"/api/v1/balance", "http://cabinet:8080", "/api/v1/balance"},
		{"/unknown", "", ""},
	}

	for _, tt := range tests {
		target, path := h.resolveTarget(tt.path)
		if tt.expectHost == "" {
			if target != nil {
				t.Errorf("%s: expected nil target, got %v", tt.path, target)
			}
			continue
		}
		if target == nil {
			t.Errorf("%s: expected target, got nil", tt.path)
			continue
		}
		if target.Host != strings.TrimPrefix(tt.expectHost, "http://") {
			t.Errorf("%s: expected host %s, got %s", tt.path, tt.expectHost, target.Host)
		}
		if path != tt.expectPath {
			t.Errorf("%s: expected path %s, got %s", tt.path, tt.expectPath, path)
		}
	}
}

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	if !strings.HasPrefix(id1, "req_") {
		t.Errorf("Expected prefix req_, got %s", id1)
	}
}
