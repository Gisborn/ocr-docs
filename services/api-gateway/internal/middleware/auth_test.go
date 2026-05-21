package middleware

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scan.passport.local/api/services/api-gateway/pkg/models"
	"golang.org/x/crypto/bcrypt"
)

// MockRepository мок репозитория
type MockRepository struct {
	apiKeys       map[int64]*models.APIKey
	organizations map[int64]*models.Organization
}

func NewMockRepository() *MockRepository {
	return &MockRepository{
		apiKeys:       make(map[int64]*models.APIKey),
		organizations: make(map[int64]*models.Organization),
	}
}

func (m *MockRepository) GetAPIKeyByID(ctx context.Context, id int64) (*models.APIKey, error) {
	return m.apiKeys[id], nil
}

func (m *MockRepository) GetOrganization(ctx context.Context, id int64) (*models.Organization, error) {
	return m.organizations[id], nil
}

func (m *MockRepository) UpdateAPIKeyLastUsed(ctx context.Context, keyID int64) error {
	if key, ok := m.apiKeys[keyID]; ok {
		now := time.Now()
		key.LastUsedAt = &now
	}
	return nil
}

func TestParseAPIKey(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		wantID    int64
		wantSecret string
		wantErr   bool
	}{
		{
			name:      "Valid key",
			apiKey:    base64.StdEncoding.EncodeToString([]byte("123:secret_key_abc")),
			wantID:    123,
			wantSecret: "secret_key_abc",
			wantErr:   false,
		},
		{
			name:    "Invalid base64",
			apiKey:  "not-valid-base64!!!",
			wantErr: true,
		},
		{
			name:    "Missing separator",
			apiKey:  base64.StdEncoding.EncodeToString([]byte("123secret")),
			wantErr: true,
		},
		{
			name:    "Invalid key ID",
			apiKey:  base64.StdEncoding.EncodeToString([]byte("abc:secret")),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, secret, err := ParseAPIKey(tt.apiKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAPIKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if id != tt.wantID {
					t.Errorf("ParseAPIKey() id = %v, want %v", id, tt.wantID)
				}
				if secret != tt.wantSecret {
					t.Errorf("ParseAPIKey() secret = %v, want %v", secret, tt.wantSecret)
				}
			}
		})
	}
}

func TestGenerateAPIKey(t *testing.T) {
	fullKey, hash, err := GenerateAPIKey(456, "my_secret_key")
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	// Проверяем что можно распарсить
	id, secret, err := ParseAPIKey(fullKey)
	if err != nil {
		t.Fatalf("ParseAPIKey() error = %v", err)
	}

	if id != 456 {
		t.Errorf("ParseAPIKey() id = %v, want 456", id)
	}

	if secret != "my_secret_key" {
		t.Errorf("ParseAPIKey() secret = %v, want my_secret_key", secret)
	}

	// Проверяем что хеш валидный (хешируется полный base64 ключ, как в cabinet service)
	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(fullKey))
	if err != nil {
		t.Errorf("bcrypt hash invalid: %v", err)
	}
}

func TestAuthMiddleware_ValidKey(t *testing.T) {
	repo := NewMockRepository()
	
	// Создаем тестовую организацию
	repo.organizations[1] = &models.Organization{
		ID:     1,
		Name:   "Test Org",
		Status: "active",
	}

	// Создаем API ключ с bcrypt хешем
	_, hash, _ := GenerateAPIKey(1, "test_secret")
	repo.apiKeys[1] = &models.APIKey{
		ID:             1,
		OrganizationID: 1,
		KeyHash:        hash,
		Name:           "Test Key",
		Status:         "active",
		RateLimitRPS:   10,
		CreatedAt:      time.Now(),
	}

	middleware := NewAuthMiddleware(repo)
	
	// Создаем тестовый handler который проверяет контекст
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKeyID := r.Context().Value(ContextKeyAPIKeyID)
		if apiKeyID == nil {
			t.Error("Expected api_key_id in context")
		}
		if apiKeyID.(int64) != 1 {
			t.Errorf("Expected api_key_id = 1, got %v", apiKeyID)
		}
		w.WriteHeader(http.StatusOK)
	})

	// Создаем запрос с валидным ключом
	fullKey, _, _ := GenerateAPIKey(1, "test_secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("X-Api-Key", fullKey)
	rec := httptest.NewRecorder()

	// Выполняем
	middleware.Handler(testHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddleware_MissingKey(t *testing.T) {
	repo := NewMockRepository()
	middleware := NewAuthMiddleware(repo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	rec := httptest.NewRecorder()

	middleware.Handler(testHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidKey(t *testing.T) {
	repo := NewMockRepository()
	middleware := NewAuthMiddleware(repo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	})

	// Запрос с несуществующим ключом
	fullKey, _, _ := GenerateAPIKey(999, "wrong_secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("X-Api-Key", fullKey)
	rec := httptest.NewRecorder()

	middleware.Handler(testHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_RevokedKey(t *testing.T) {
	repo := NewMockRepository()
	
	repo.organizations[1] = &models.Organization{
		ID:     1,
		Name:   "Test Org",
		Status: "active",
	}

	_, hash, _ := GenerateAPIKey(1, "test_secret")
	repo.apiKeys[1] = &models.APIKey{
		ID:             1,
		OrganizationID: 1,
		KeyHash:        hash,
		Name:           "Test Key",
		Status:         "revoked", // Отозван
		RateLimitRPS:   10,
		CreatedAt:      time.Now(),
	}

	middleware := NewAuthMiddleware(repo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	})

	fullKey, _, _ := GenerateAPIKey(1, "test_secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("X-Api-Key", fullKey)
	rec := httptest.NewRecorder()

	middleware.Handler(testHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InactiveOrg(t *testing.T) {
	repo := NewMockRepository()
	
	repo.organizations[1] = &models.Organization{
		ID:     1,
		Name:   "Test Org",
		Status: "blocked", // Заблокирована
	}

	_, hash, _ := GenerateAPIKey(1, "test_secret")
	repo.apiKeys[1] = &models.APIKey{
		ID:             1,
		OrganizationID: 1,
		KeyHash:        hash,
		Name:           "Test Key",
		Status:         "active",
		RateLimitRPS:   10,
		CreatedAt:      time.Now(),
	}

	middleware := NewAuthMiddleware(repo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	})

	fullKey, _, _ := GenerateAPIKey(1, "test_secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("X-Api-Key", fullKey)
	rec := httptest.NewRecorder()

	middleware.Handler(testHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}

func TestExemptPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/health", true},
		{"/webhooks/yookassa", true},
		{"/v1/webhooks/test", true},
		{"/v1/recognize", false},
		{"/v1/billing/balance", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ExemptPath(tt.path); got != tt.want {
				t.Errorf("ExemptPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
