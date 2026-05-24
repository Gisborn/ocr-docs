package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"scan.passport.local/api/services/cabinet/internal/middleware"
	"scan.passport.local/api/services/cabinet/internal/service"
	"scan.passport.local/api/services/cabinet/pkg/models"
)

func setupTestHandler() (*Handler, *service.MockRepository) {
	repo := service.NewMockRepository()

	// Billing mock server (for auth service registration)
	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/accounts" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]int64{"id": 999})
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/accounts/") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"real_balance_rub":    1000,
				"prepaid_balance_rub": 0,
			})
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/accounts/") && strings.Contains(r.URL.Path, "/events") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	authSvc := service.NewAuthService(repo, billingServer.URL, "test-token")
	apiKeySvc := service.NewAPIKeyService(repo)
	// PaymentService and SubscriptionService require real DB, skip for unit tests
	// We pass nil and avoid calling those endpoints in unit tests
	var paySvc *service.PaymentService
	var subSvc *service.SubscriptionService

	h := NewHandler(authSvc, apiKeySvc, paySvc, subSvc)
	return h, repo
}

func TestHandler_Health(t *testing.T) {
	h, _ := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestHandler_Register(t *testing.T) {
	h, repo := setupTestHandler()

	body, _ := json.Marshal(service.RegisterRequest{
		OrganizationName: "Test Org",
		Email:            "test_register@example.com",
		Password:         "password123",
		AcceptedTerms:    true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp service.RegisterResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.OrgID == 0 {
		t.Fatal("Expected org_id in response")
	}

	// Verify org created in repo
	org, _ := repo.GetOrganizationByID(context.Background(), resp.OrgID)
	if org == nil {
		t.Fatal("Organization not created in repository")
	}
}

func TestHandler_RegisterInvalidEmail(t *testing.T) {
	h, _ := setupTestHandler()

	body, _ := json.Marshal(service.RegisterRequest{
		OrganizationName: "Test Org",
		Email:            "invalid-email",
		Password:         "password123",
		AcceptedTerms:    true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}

func TestHandler_RegisterDuplicateEmail(t *testing.T) {
	h, _ := setupTestHandler()

	body, _ := json.Marshal(service.RegisterRequest{
		OrganizationName: "Test Org",
		Email:            "dup@example.com",
		Password:         "password123",
		AcceptedTerms:    true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
	h.Register(httptest.NewRecorder(), req)

	// Second registration with same email
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for duplicate email, got %d", rr.Code)
	}
}

func TestHandler_RegisterShortPassword(t *testing.T) {
	h, _ := setupTestHandler()

	body, _ := json.Marshal(service.RegisterRequest{
		OrganizationName: "Test Org",
		Email:            "short@example.com",
		Password:         "123",
		AcceptedTerms:    true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for short password, got %d", rr.Code)
	}
}

func TestHandler_Login(t *testing.T) {
	h, repo := setupTestHandler()

	// Register first
	body, _ := json.Marshal(service.RegisterRequest{
		OrganizationName: "Login Test Org",
		Email:            "login_test@example.com",
		Password:         "password123",
		AcceptedTerms:    true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
	h.Register(httptest.NewRecorder(), req)

	// Activate email (bypass verification for test)
	org, _ := repo.GetOrganizationByEmail(context.Background(), "login_test@example.com")
	if org != nil {
		org.EmailVerified = true
		repo.UpdateOrganization(context.Background(), org)
	}

	// Login
	loginBody, _ := json.Marshal(service.LoginRequest{
		Email:    "login_test@example.com",
		Password: "password123",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(loginBody))
	rr := httptest.NewRecorder()

	h.Login(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp service.LoginResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.SessionToken == "" {
		t.Fatal("Expected session_token in response")
	}

	// Verify session exists
	sessions, _ := repo.GetSessionByToken(context.Background(), resp.SessionToken)
	if sessions == nil {
		t.Fatal("Session not created")
	}
}

func TestHandler_LoginInvalidCredentials(t *testing.T) {
	h, _ := setupTestHandler()

	body, _ := json.Marshal(service.LoginRequest{
		Email:    "nonexistent@example.com",
		Password: "wrongpassword",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.Login(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rr.Code)
	}
}

func TestHandler_Logout(t *testing.T) {
	h, repo := setupTestHandler()

	// Create session
	sess := &models.Session{
		Token:     "logout_token",
		UserID:    1,
		OrgID:     1,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	repo.CreateSession(context.Background(), sess)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "logout_token"})
	rr := httptest.NewRecorder()

	h.Logout(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	// Verify session deleted
	_, err := repo.GetSessionByToken(context.Background(), "logout_token")
	if err == nil {
		t.Error("Expected session to be deleted after logout")
	}
}

func TestHandler_Verify(t *testing.T) {
	h, repo := setupTestHandler()

	// Create org and session
	orgID, userID, token := repo.SeedTestData()
	_ = orgID

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/verify", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserID, userID))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyOrgID, int64(1)))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyBillingAccountID, int64(1)))
	rr := httptest.NewRecorder()

	h.Verify(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["user_id"] == nil {
		t.Error("Expected user_id in response")
	}
	_ = token
}

func TestHandler_VerifyUnauthorized(t *testing.T) {
	h, _ := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/verify", nil)
	rr := httptest.NewRecorder()

	h.Verify(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rr.Code)
	}
}

func TestHandler_CreateAPIKey(t *testing.T) {
	h, repo := setupTestHandler()

	orgID, userID, _ := repo.SeedTestData()

	body, _ := json.Marshal(service.CreateAPIKeyRequest{Name: "Test Key"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", bytes.NewBuffer(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyOrgID, orgID))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserID, userID))
	rr := httptest.NewRecorder()

	h.CreateAPIKey(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp service.CreateAPIKeyResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.FullKey == "" {
		t.Error("Expected full_key in response")
	}
}

func TestHandler_CreateAPIKeyUnauthorized(t *testing.T) {
	h, _ := setupTestHandler()

	body, _ := json.Marshal(service.CreateAPIKeyRequest{Name: "Test Key"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	h.CreateAPIKey(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rr.Code)
	}
}

func TestHandler_ListAPIKeys(t *testing.T) {
	h, repo := setupTestHandler()

	orgID, userID, _ := repo.SeedTestData()

	// Create a key first
	body, _ := json.Marshal(service.CreateAPIKeyRequest{Name: "Key 1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", bytes.NewBuffer(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyOrgID, orgID))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserID, userID))
	h.CreateAPIKey(httptest.NewRecorder(), req)

	// List keys
	req = httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyOrgID, orgID))
	rr := httptest.NewRecorder()

	h.ListAPIKeys(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp []service.APIKeyInfo
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Errorf("Expected 1 key, got %d", len(resp))
	}
}

func TestHandler_RevokeAPIKey(t *testing.T) {
	h, repo := setupTestHandler()

	orgID, userID, _ := repo.SeedTestData()

	// Create a key
	body, _ := json.Marshal(service.CreateAPIKeyRequest{Name: "Key to revoke"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", bytes.NewBuffer(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyOrgID, orgID))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserID, userID))
	rr := httptest.NewRecorder()
	h.CreateAPIKey(rr, req)

	var createResp service.CreateAPIKeyResponse
	json.NewDecoder(rr.Body).Decode(&createResp)

	// Revoke
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/api-keys/%d", createResp.ID), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyOrgID, orgID))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserID, userID))
	rr = httptest.NewRecorder()

	h.RevokeAPIKey(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify revoked
	key, _ := repo.GetAPIKeyByID(context.Background(), createResp.ID)
	if key.Status != "revoked" {
		t.Errorf("Expected status revoked, got %s", key.Status)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h, _ := setupTestHandler()

	endpoints := []struct {
		method string
		path   string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{http.MethodGet, "/api/v1/auth/register", h.Register},
		{http.MethodGet, "/api/v1/auth/login", h.Login},
		{http.MethodPost, "/api/v1/auth/verify", h.Verify},
		{http.MethodGet, "/api/v1/auth/logout", h.Logout},
		{http.MethodGet, "/api/v1/api-keys", h.CreateAPIKey},
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		rr := httptest.NewRecorder()
		ep.handler(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: expected 405, got %d", ep.method, ep.path, rr.Code)
		}
	}
}
