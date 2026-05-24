package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"scan.passport.local/api/pkg/testdb"
	"scan.passport.local/api/services/cabinet/pkg/models"
)

func setupCabinetRepo(t *testing.T) (*PostgresRepository, *pgxpool.Pool) {
	pool := testdb.MustPool(t, testdb.DefaultMainURL())
	testdb.ApplyMigrations(t, pool, "../../../../migrations/main")
	testdb.Cleanup(t, pool, "account_events", "sessions", "api_keys", "users", "organizations")
	return NewPostgresRepository(pool), pool
}

func TestPostgresRepository_Organizations(t *testing.T) {
	repo, _ := setupCabinetRepo(t)
	ctx := context.Background()

	org := &models.Organization{
		Name:            "Test Org",
		Email:           "org@example.com",
		PasswordHash:    "hash",
		Status:          "active",
		AcceptedTermsAt: timePtr(time.Now()),
	}
	err := repo.CreateOrganization(ctx, org)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if org.ID == 0 {
		t.Fatal("expected org id")
	}

	// Get by email
	found, err := repo.GetOrganizationByEmail(ctx, "org@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected org")
	}
	if found.Name != "Test Org" {
		t.Fatalf("expected name Test Org, got %s", found.Name)
	}

	// Get by ID
	byID, err := repo.GetOrganizationByID(ctx, org.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if byID.ID != org.ID {
		t.Fatalf("expected id %d, got %d", org.ID, byID.ID)
	}

	// Update
	org.Name = "Updated Org"
	org.Status = "inactive"
	err = repo.UpdateOrganization(ctx, org)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	updated, _ := repo.GetOrganizationByID(ctx, org.ID)
	if updated.Name != "Updated Org" {
		t.Fatalf("expected name Updated Org, got %s", updated.Name)
	}
	if updated.Status != "inactive" {
		t.Fatalf("expected status inactive, got %s", updated.Status)
	}

	// SetBillingAccountID
	err = repo.SetBillingAccountID(ctx, org.ID, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	withBilling, _ := repo.GetOrganizationByID(ctx, org.ID)
	if withBilling.BillingAccountID == nil || *withBilling.BillingAccountID != 999 {
		t.Fatalf("expected billing account id 999, got %v", withBilling.BillingAccountID)
	}

	// Not found
	notFound, err := repo.GetOrganizationByEmail(ctx, "none@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notFound != nil {
		t.Fatal("expected nil")
	}
}

func TestPostgresRepository_Users(t *testing.T) {
	repo, _ := setupCabinetRepo(t)
	ctx := context.Background()

	org := &models.Organization{Name: "Test", Email: "u@example.com", PasswordHash: "hash", Status: "active"}
	repo.CreateOrganization(ctx, org)

	user := &models.User{
		OrgID:        org.ID,
		Email:        "u@example.com",
		PasswordHash: "hash",
		Role:         "admin",
	}
	err := repo.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("expected user id")
	}

	// Get by email
	found, err := repo.GetUserByEmail(ctx, org.ID, "u@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected user")
	}
	if found.Role != "admin" {
		t.Fatalf("expected role admin, got %s", found.Role)
	}

	// Get by ID
	byID, err := repo.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if byID.ID != user.ID {
		t.Fatalf("expected id %d, got %d", user.ID, byID.ID)
	}

	// Update
	user.Email = "new@example.com"
	user.Role = "user"
	err = repo.UpdateUser(ctx, user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	updated, _ := repo.GetUserByID(ctx, user.ID)
	if updated.Email != "new@example.com" {
		t.Fatalf("expected email new@example.com, got %s", updated.Email)
	}
	if updated.Role != "user" {
		t.Fatalf("expected role user, got %s", updated.Role)
	}

	// UpdateLastLogin
	err = repo.UpdateLastLogin(ctx, user.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	withLogin, _ := repo.GetUserByID(ctx, user.ID)
	if withLogin.LastLoginAt == nil {
		t.Fatal("expected last_login_at")
	}
}

func TestPostgresRepository_APIKeys(t *testing.T) {
	repo, _ := setupCabinetRepo(t)
	ctx := context.Background()

	org := &models.Organization{Name: "Test", Email: "k@example.com", PasswordHash: "hash", Status: "active"}
	repo.CreateOrganization(ctx, org)

	key := &models.APIKey{
		OrgID:        org.ID,
		Name:         "Test Key",
		KeyHash:      "hash",
		Status:       "active",
		RateLimitRPS: 10,
	}
	err := repo.CreateAPIKey(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.ID == 0 {
		t.Fatal("expected key id")
	}

	// Get by ID
	found, err := repo.GetAPIKeyByID(ctx, key.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected key")
	}
	if found.Name != "Test Key" {
		t.Fatalf("expected name Test Key, got %s", found.Name)
	}

	// List
	keys, err := repo.ListAPIKeys(ctx, org.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	// Count active
	count, err := repo.CountActiveAPIKeys(ctx, org.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	// Update hash
	err = repo.UpdateAPIKeyHash(ctx, key.ID, "newhash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	updated, _ := repo.GetAPIKeyByID(ctx, key.ID)
	if updated.KeyHash != "newhash" {
		t.Fatalf("expected hash newhash, got %s", updated.KeyHash)
	}

	// Revoke
	err = repo.RevokeAPIKey(ctx, key.ID, org.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count, _ = repo.CountActiveAPIKeys(ctx, org.ID)
	if count != 0 {
		t.Fatalf("expected count 0 after revoke, got %d", count)
	}

	// Revoke wrong org
	err = repo.RevokeAPIKey(ctx, key.ID, 99999)
	if err == nil {
		t.Fatal("expected error for wrong org")
	}
}

func TestPostgresRepository_Sessions(t *testing.T) {
	repo, _ := setupCabinetRepo(t)
	ctx := context.Background()

	org := &models.Organization{Name: "Test", Email: "s@example.com", PasswordHash: "hash", Status: "active"}
	repo.CreateOrganization(ctx, org)
	user := &models.User{OrgID: org.ID, Email: "s@example.com", PasswordHash: "hash", Role: "admin"}
	repo.CreateUser(ctx, user)

	session := &models.Session{
		ID:               "sess_1",
		UserID:           user.ID,
		OrgID:            org.ID,
		BillingAccountID: 1,
		Token:            "token_123",
		ExpiresAt:        time.Now().Add(24 * time.Hour),
	}
	err := repo.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get by token
	found, err := repo.GetSessionByToken(ctx, "token_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected session")
	}
	if found.Token != "token_123" {
		t.Fatalf("expected token token_123, got %s", found.Token)
	}

	// Expired session
	expired := &models.Session{
		ID:               "sess_2",
		UserID:           user.ID,
		OrgID:            org.ID,
		BillingAccountID: 1,
		Token:            "token_expired",
		ExpiresAt:        time.Now().Add(-time.Hour),
	}
	repo.CreateSession(ctx, expired)
	notFound, _ := repo.GetSessionByToken(ctx, "token_expired")
	if notFound != nil {
		t.Fatal("expected nil for expired session")
	}

	// Delete
	err = repo.DeleteSession(ctx, "token_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	deleted, _ := repo.GetSessionByToken(ctx, "token_123")
	if deleted != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestPostgresRepository_AccountEvents(t *testing.T) {
	repo, _ := setupCabinetRepo(t)
	ctx := context.Background()

	org := &models.Organization{Name: "Test", Email: "e@example.com", PasswordHash: "hash", Status: "active"}
	repo.CreateOrganization(ctx, org)

	event := &models.AccountEvent{
		OrgID:     org.ID,
		EventType: "test_event",
		Payload:   map[string]interface{}{"key": "value"},
		ActorID:   nil,
	}
	err := repo.CreateAccountEvent(ctx, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// List all
	events, err := repo.ListAccountEvents(ctx, org.ID, "", time.Time{}, time.Time{}, 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Filter by type
	events, _ = repo.ListAccountEvents(ctx, org.ID, "test_event", time.Time{}, time.Time{}, 10, 0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event by type, got %d", len(events))
	}

	// Filter by wrong type
	events, _ = repo.ListAccountEvents(ctx, org.ID, "wrong", time.Time{}, time.Time{}, 10, 0)
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	// Limit
	repo.CreateAccountEvent(ctx, &models.AccountEvent{OrgID: org.ID, EventType: "e2"})
	repo.CreateAccountEvent(ctx, &models.AccountEvent{OrgID: org.ID, EventType: "e3"})
	events, _ = repo.ListAccountEvents(ctx, org.ID, "", time.Time{}, time.Time{}, 2, 0)
	if len(events) != 2 {
		t.Fatalf("expected 2 events with limit, got %d", len(events))
	}

	// Offset
	events, _ = repo.ListAccountEvents(ctx, org.ID, "", time.Time{}, time.Time{}, 10, 2)
	if len(events) != 1 {
		t.Fatalf("expected 1 event with offset, got %d", len(events))
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
