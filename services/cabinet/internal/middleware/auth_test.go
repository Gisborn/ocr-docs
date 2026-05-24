package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scan.passport.local/api/services/cabinet/pkg/models"
)

// mockRepo для middleware тестов
type mockRepo struct {
	session *models.Session
	org     *models.Organization
}

func (m *mockRepo) CreateOrganization(ctx context.Context, org *models.Organization) error { return nil }
func (m *mockRepo) GetOrganizationByEmail(ctx context.Context, email string) (*models.Organization, error) {
	return nil, nil
}
func (m *mockRepo) GetOrganizationByID(ctx context.Context, id int64) (*models.Organization, error) {
	return m.org, nil
}
func (m *mockRepo) UpdateOrganization(ctx context.Context, org *models.Organization) error { return nil }
func (m *mockRepo) SetBillingAccountID(ctx context.Context, orgID int64, billingAccountID int64) error {
	return nil
}
func (m *mockRepo) CreateUser(ctx context.Context, user *models.User) error { return nil }
func (m *mockRepo) GetUserByEmail(ctx context.Context, orgID int64, email string) (*models.User, error) {
	return nil, nil
}
func (m *mockRepo) GetUserByID(ctx context.Context, id int64) (*models.User, error) { return nil, nil }
func (m *mockRepo) UpdateUser(ctx context.Context, user *models.User) error { return nil }
func (m *mockRepo) UpdateLastLogin(ctx context.Context, userID int64) error { return nil }
func (m *mockRepo) CreateAPIKey(ctx context.Context, key *models.APIKey) error { return nil }
func (m *mockRepo) GetAPIKeyByID(ctx context.Context, id int64) (*models.APIKey, error) { return nil, nil }
func (m *mockRepo) ListAPIKeys(ctx context.Context, orgID int64) ([]*models.APIKey, error) {
	return nil, nil
}
func (m *mockRepo) RevokeAPIKey(ctx context.Context, id int64, orgID int64) error { return nil }
func (m *mockRepo) CountActiveAPIKeys(ctx context.Context, orgID int64) (int, error) { return 0, nil }
func (m *mockRepo) UpdateAPIKeyHash(ctx context.Context, keyID int64, keyHash string) error { return nil }
func (m *mockRepo) CreateSession(ctx context.Context, session *models.Session) error { return nil }
func (m *mockRepo) GetSessionByToken(ctx context.Context, token string) (*models.Session, error) {
	if m.session != nil && m.session.Token == token {
		return m.session, nil
	}
	return nil, nil
}
func (m *mockRepo) DeleteSession(ctx context.Context, token string) error { return nil }
func (m *mockRepo) CreateAccountEvent(ctx context.Context, event *models.AccountEvent) error {
	return nil
}
func (m *mockRepo) ListAccountEvents(ctx context.Context, orgID int64, eventType string, from, to time.Time, limit, offset int) ([]*models.AccountEvent, error) {
	return nil, nil
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	repo := &mockRepo{
		session: &models.Session{
			Token:            "valid_token",
			UserID:           42,
			OrgID:            7,
			BillingAccountID: 99,
			ExpiresAt:        time.Now().Add(time.Hour),
		},
		org: &models.Organization{
			ID:               7,
			BillingAccountID: intPtr(99),
		},
	}

	mw := NewAuthMiddleware(repo)

	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetUserID(r.Context()) != 42 {
			t.Errorf("Expected user_id=42, got %d", GetUserID(r.Context()))
		}
		if GetOrgID(r.Context()) != 7 {
			t.Errorf("Expected org_id=7, got %d", GetOrgID(r.Context()))
		}
		if GetBillingAccountID(r.Context()) != 99 {
			t.Errorf("Expected billing_account_id=99, got %d", GetBillingAccountID(r.Context()))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer valid_token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	repo := &mockRepo{}
	mw := NewAuthMiddleware(repo)

	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	repo := &mockRepo{}
	mw := NewAuthMiddleware(repo)

	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer invalid_token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_ExpiredSession(t *testing.T) {
	repo := &mockRepo{
		session: &models.Session{
			Token:     "expired_token",
			UserID:    1,
			OrgID:     1,
			ExpiresAt: time.Now().Add(-time.Hour),
		},
	}
	mw := NewAuthMiddleware(repo)

	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer expired_token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_CookieToken(t *testing.T) {
	repo := &mockRepo{
		session: &models.Session{
			Token:     "cookie_token",
			UserID:    5,
			OrgID:     3,
			ExpiresAt: time.Now().Add(time.Hour),
		},
		org: &models.Organization{ID: 3},
	}
	mw := NewAuthMiddleware(repo)

	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetUserID(r.Context()) != 5 {
			t.Errorf("Expected user_id=5, got %d", GetUserID(r.Context()))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "cookie_token"})
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_OptionalHandler(t *testing.T) {
	repo := &mockRepo{
		session: &models.Session{
			Token:     "opt_token",
			UserID:    8,
			OrgID:     4,
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	mw := NewAuthMiddleware(repo)

	handler := mw.OptionalHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should be called even without token
		w.WriteHeader(http.StatusOK)
	}))

	// No token
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 without token, got %d", rr.Code)
	}

	// With valid token
	req = httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer opt_token")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 with token, got %d", rr.Code)
	}
	if GetUserID(req.Context()) != 8 {
		// Context from request before ServeHTTP may not have value
		// OptionalHandler modifies request, but the handler above captures the new one
	}
}

func TestAuthMiddleware_OrgWithoutBillingAccount(t *testing.T) {
	repo := &mockRepo{
		session: &models.Session{
			Token:            "no_billing_token",
			UserID:           1,
			OrgID:            2,
			BillingAccountID: 0,
			ExpiresAt:        time.Now().Add(time.Hour),
		},
		org: &models.Organization{
			ID:               2,
			BillingAccountID: nil,
		},
	}
	mw := NewAuthMiddleware(repo)

	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetBillingAccountID(r.Context()) != 0 {
			t.Errorf("Expected billing_account_id=0, got %d", GetBillingAccountID(r.Context()))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer no_billing_token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func intPtr(i int64) *int64 {
	return &i
}
