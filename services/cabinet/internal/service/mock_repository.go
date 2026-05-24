package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"scan.passport.local/api/services/cabinet/pkg/models"
)

// MockRepository мок репозитория Cabinet для тестирования
type MockRepository struct {
	mu              sync.RWMutex
	orgs            map[int64]*models.Organization
	users           map[int64]*models.User
	apiKeys         map[int64]*models.APIKey
	sessions        map[string]*models.Session
	events          []*models.AccountEvent
	nextOrgID       int64
	nextUserID      int64
	nextKeyID       int64
	nextEventID     int64
}

// NewMockRepository создает новый мок репозитория
func NewMockRepository() *MockRepository {
	return &MockRepository{
		orgs:     make(map[int64]*models.Organization),
		users:    make(map[int64]*models.User),
		apiKeys:  make(map[int64]*models.APIKey),
		sessions: make(map[string]*models.Session),
		events:   make([]*models.AccountEvent, 0),
		nextOrgID:  1,
		nextUserID: 1,
		nextKeyID:  1,
	}
}

// Organizations

func (m *MockRepository) CreateOrganization(ctx context.Context, org *models.Organization) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	org.ID = m.nextOrgID
	org.CreatedAt = time.Now()
	org.UpdatedAt = time.Now()
	m.orgs[org.ID] = org
	m.nextOrgID++
	return nil
}

func (m *MockRepository) GetOrganizationByEmail(ctx context.Context, email string) (*models.Organization, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, org := range m.orgs {
		if strings.EqualFold(org.Email, email) {
			return org, nil
		}
	}
	return nil, nil
}

func (m *MockRepository) GetOrganizationByID(ctx context.Context, id int64) (*models.Organization, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	org, ok := m.orgs[id]
	if !ok {
		return nil, fmt.Errorf("organization not found")
	}
	return org, nil
}

func (m *MockRepository) UpdateOrganization(ctx context.Context, org *models.Organization) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	org.UpdatedAt = time.Now()
	m.orgs[org.ID] = org
	return nil
}

func (m *MockRepository) SetBillingAccountID(ctx context.Context, orgID int64, billingAccountID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if org, ok := m.orgs[orgID]; ok {
		org.BillingAccountID = &billingAccountID
	}
	return nil
}

// Users

func (m *MockRepository) CreateUser(ctx context.Context, user *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user.ID = m.nextUserID
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()
	m.users[user.ID] = user
	m.nextUserID++
	return nil
}

func (m *MockRepository) GetUserByEmail(ctx context.Context, orgID int64, email string) (*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.OrgID == orgID && strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, nil
}

func (m *MockRepository) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	return u, nil
}

func (m *MockRepository) UpdateUser(ctx context.Context, user *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user.UpdatedAt = time.Now()
	m.users[user.ID] = user
	return nil
}

func (m *MockRepository) UpdateLastLogin(ctx context.Context, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[userID]; ok {
		t := time.Now()
		u.LastLoginAt = &t
	}
	return nil
}

// API Keys

func (m *MockRepository) CreateAPIKey(ctx context.Context, key *models.APIKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key.ID = m.nextKeyID
	key.CreatedAt = time.Now()
	m.apiKeys[key.ID] = key
	m.nextKeyID++
	return nil
}

func (m *MockRepository) GetAPIKeyByID(ctx context.Context, id int64) (*models.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.apiKeys[id]
	if !ok {
		return nil, fmt.Errorf("api key not found")
	}
	return key, nil
}

func (m *MockRepository) ListAPIKeys(ctx context.Context, orgID int64) ([]*models.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*models.APIKey
	for _, key := range m.apiKeys {
		if key.OrgID == orgID && key.Status == "active" {
			result = append(result, key)
		}
	}
	return result, nil
}

func (m *MockRepository) RevokeAPIKey(ctx context.Context, id int64, orgID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key, ok := m.apiKeys[id]; ok && key.OrgID == orgID {
		key.Status = "revoked"
	}
	return nil
}

func (m *MockRepository) CountActiveAPIKeys(ctx context.Context, orgID int64) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, key := range m.apiKeys {
		if key.OrgID == orgID && key.Status == "active" {
			count++
		}
	}
	return count, nil
}

func (m *MockRepository) UpdateAPIKeyHash(ctx context.Context, keyID int64, keyHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key, ok := m.apiKeys[keyID]; ok {
		key.KeyHash = keyHash
	}
	return nil
}

// Sessions

func (m *MockRepository) CreateSession(ctx context.Context, session *models.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.Token] = session
	return nil
}

func (m *MockRepository) GetSessionByToken(ctx context.Context, token string) (*models.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[token]
	if !ok || sess.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("session not found or expired")
	}
	return sess, nil
}

func (m *MockRepository) DeleteSession(ctx context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
	return nil
}

// Account Events

func (m *MockRepository) CreateAccountEvent(ctx context.Context, event *models.AccountEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	event.ID = m.nextEventID
	event.CreatedAt = time.Now()
	m.events = append(m.events, event)
	m.nextEventID++
	return nil
}

func (m *MockRepository) ListAccountEvents(ctx context.Context, orgID int64, eventType string, from, to time.Time, limit, offset int) ([]*models.AccountEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*models.AccountEvent
	for _, e := range m.events {
		if e.OrgID != orgID {
			continue
		}
		if eventType != "" && e.EventType != eventType {
			continue
		}
		if !from.IsZero() && e.CreatedAt.Before(from) {
			continue
		}
		if !to.IsZero() && e.CreatedAt.After(to) {
			continue
		}
		result = append(result, e)
	}
	// Simple pagination
	if offset >= len(result) {
		return []*models.AccountEvent{}, nil
	}
	end := offset + limit
	if end > len(result) || limit <= 0 {
		end = len(result)
	}
	return result[offset:end], nil
}

// SeedTestData создает тестовую организацию и пользователя
func (m *MockRepository) SeedTestData() (orgID, userID int64, token string) {
	ctx := context.Background()
	org := &models.Organization{
		Name:          "Test Org",
		Email:         "test@example.com",
		EmailVerified: true,
		PasswordHash:  "$2a$10$kIxY6tX2MRiV4tROQZHKOenezw37Hdc1s14qDCSy9jsqBYFDP2Xde", // bcrypt hash for "password"
		Status:        "active",
	}
	m.CreateOrganization(ctx, org)
	orgID = org.ID

	user := &models.User{
		OrgID:        orgID,
		Email:        "test@example.com",
		PasswordHash: org.PasswordHash,
		Role:         "admin",
	}
	m.CreateUser(ctx, user)
	userID = user.ID

	sess := &models.Session{
		ID:               "sess_001",
		UserID:           userID,
		OrgID:            orgID,
		BillingAccountID: 1,
		Token:            "valid_session_token",
		ExpiresAt:        time.Now().Add(24 * time.Hour),
	}
	m.CreateSession(ctx, sess)
	token = sess.Token

	return
}
