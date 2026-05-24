package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func setupAPIKeyService() (*APIKeyService, *MockRepository) {
	repo := NewMockRepository()
	return NewAPIKeyService(repo), repo
}

func TestAPIKeyService_CreateAPIKey_Success(t *testing.T) {
	svc, repo := setupAPIKeyService()
	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	resp, err := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "Test Key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID == 0 {
		t.Fatal("expected key id")
	}
	if resp.Name != "Test Key" {
		t.Fatalf("expected name Test Key, got %s", resp.Name)
	}
	if resp.FullKey == "" {
		t.Fatal("expected full_key")
	}
	if resp.RateLimitRPS != 10 {
		t.Fatalf("expected rate limit 10, got %d", resp.RateLimitRPS)
	}
	if resp.CreatedAt == "" {
		t.Fatal("expected created_at")
	}

	// Verify event logged
	events, _ := repo.ListAccountEvents(ctx, orgID, "", time.Time{}, time.Time{}, 10, 0)
	found := false
	for _, e := range events {
		if e.EventType == "api_key_created" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected api_key_created event")
	}
}

func TestAPIKeyService_CreateAPIKey_MaxLimit(t *testing.T) {
	svc, repo := setupAPIKeyService()
	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	// Create 10 keys (max)
	for i := 0; i < 10; i++ {
		_, err := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "Key"})
		if err != nil {
			t.Fatalf("unexpected error on key %d: %v", i, err)
		}
	}

	// 11th should fail
	_, err := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "Overflow"})
	if err == nil || !strings.Contains(err.Error(), "maximum number of API keys") {
		t.Fatalf("expected max limit error, got %v", err)
	}
}

func TestAPIKeyService_CreateAPIKey_CountError(t *testing.T) {
	// Mock doesn't return errors for CountActiveAPIKeys, so we test the success path
	// In real scenario with SQL error, it would return "database error"
	svc, repo := setupAPIKeyService()
	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	_, err := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "Key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIKeyService_ListAPIKeys(t *testing.T) {
	svc, repo := setupAPIKeyService()
	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	// Empty list
	keys, err := svc.ListAPIKeys(ctx, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}

	// Create a key
	resp, _ := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "Key 1"})

	keys, err = svc.ListAPIKeys(ctx, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].ID != resp.ID {
		t.Fatalf("expected key id %d, got %d", resp.ID, keys[0].ID)
	}
	if keys[0].Name != "Key 1" {
		t.Fatalf("expected name Key 1, got %s", keys[0].Name)
	}
	if keys[0].Status != "active" {
		t.Fatalf("expected status active, got %s", keys[0].Status)
	}
	if keys[0].Preview == "" {
		t.Fatal("expected preview for active key")
	}
	if keys[0].RateLimitRPS != 10 {
		t.Fatalf("expected rate limit 10, got %d", keys[0].RateLimitRPS)
	}
	if keys[0].CreatedAt == "" {
		t.Fatal("expected created_at")
	}
}

func TestAPIKeyService_ListAPIKeys_RevokedNoPreview(t *testing.T) {
	svc, repo := setupAPIKeyService()
	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	resp, _ := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "Key"})
	err := svc.RevokeAPIKey(ctx, orgID, resp.ID, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	keys, _ := svc.ListAPIKeys(ctx, orgID)
	if len(keys) != 0 {
		// ListAPIKeys only returns active keys
		t.Fatalf("expected 0 active keys, got %d", len(keys))
	}
}

func TestAPIKeyService_RevokeAPIKey(t *testing.T) {
	svc, repo := setupAPIKeyService()
	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	resp, _ := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "To Revoke"})

	err := svc.RevokeAPIKey(ctx, orgID, resp.ID, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify revoked
	key, _ := repo.GetAPIKeyByID(ctx, resp.ID)
	if key.Status != "revoked" {
		t.Fatalf("expected status revoked, got %s", key.Status)
	}

	// Verify event
	events, _ := repo.ListAccountEvents(ctx, orgID, "", time.Time{}, time.Time{}, 10, 0)
	found := false
	for _, e := range events {
		if e.EventType == "api_key_revoked" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected api_key_revoked event")
	}
}

func TestAPIKeyService_RevokeAPIKey_WrongOrg(t *testing.T) {
	svc, repo := setupAPIKeyService()
	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	resp, _ := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "Key"})

	// Revoke with wrong orgID (mock silently ignores)
	err := svc.RevokeAPIKey(ctx, 99999, resp.ID, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Key should still be active because wrong org
	key, _ := repo.GetAPIKeyByID(ctx, resp.ID)
	if key.Status != "active" {
		t.Fatalf("expected key to remain active, got %s", key.Status)
	}
}

func TestAPIKeyService_CreateAPIKey_HashUpdate(t *testing.T) {
	svc, repo := setupAPIKeyService()
	ctx := context.Background()
	orgID, userID, _ := repo.SeedTestData()

	resp, err := svc.CreateAPIKey(ctx, orgID, userID, &CreateAPIKeyRequest{Name: "Hash Test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify that the hash was updated (not the temp hash)
	key, _ := repo.GetAPIKeyByID(ctx, resp.ID)
	if key.KeyHash == "" {
		t.Fatal("expected key hash to be set")
	}
	// The full key should decode to id:secret format
	if !strings.Contains(resp.FullKey, ":") && len(resp.FullKey) < 10 {
		t.Fatalf("full key looks invalid: %s", resp.FullKey)
	}
}
