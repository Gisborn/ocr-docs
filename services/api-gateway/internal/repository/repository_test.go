package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"scan.passport.local/api/pkg/testdb"
)

func setupAPIGatewayRepo(t *testing.T) (*PostgresRepository, *pgxpool.Pool) {
	pool := testdb.MustPool(t, testdb.DefaultMainURL())
	testdb.ApplyMigrations(t, pool, "../../../../migrations/main")
	testdb.Cleanup(t, pool, "api_keys", "organizations", "users", "sessions", "account_events")
	return NewPostgresRepository(pool), pool
}

func TestPostgresRepository_GetAPIKeyByID(t *testing.T) {
	repo, pool := setupAPIGatewayRepo(t)
	ctx := context.Background()

	// Insert org and key
	var orgID int64
	err := pool.QueryRow(ctx, `INSERT INTO organizations (name, email, status) VALUES ('Test Org', 'test@example.com', 'active') RETURNING id`).Scan(&orgID)
	if err != nil {
		t.Fatalf("insert org: %v", err)
	}

	var keyID int64
	err = pool.QueryRow(ctx, `INSERT INTO api_keys (org_id, name, key_hash, status, rate_limit_rps) VALUES ($1, 'Test Key', 'hash', 'active', 10) RETURNING id`, orgID).Scan(&keyID)
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}

	// Found
	key, err := repo.GetAPIKeyByID(ctx, keyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected key")
	}
	if key.ID != keyID {
		t.Fatalf("expected id %d, got %d", keyID, key.ID)
	}
	if key.OrganizationID != orgID {
		t.Fatalf("expected org_id %d, got %d", orgID, key.OrganizationID)
	}
	if key.Status != "active" {
		t.Fatalf("expected status active, got %s", key.Status)
	}

	// Not found
	key, err = repo.GetAPIKeyByID(ctx, 99999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != nil {
		t.Fatal("expected nil")
	}
}

func TestPostgresRepository_GetOrganization(t *testing.T) {
	repo, pool := setupAPIGatewayRepo(t)
	ctx := context.Background()

	var orgID int64
	err := pool.QueryRow(ctx, `INSERT INTO organizations (name, email, status) VALUES ('Test Org', 'test2@example.com', 'active') RETURNING id`).Scan(&orgID)
	if err != nil {
		t.Fatalf("insert org: %v", err)
	}

	org, err := repo.GetOrganization(ctx, orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if org == nil {
		t.Fatal("expected org")
	}
	if org.ID != orgID {
		t.Fatalf("expected id %d, got %d", orgID, org.ID)
	}
	if org.Name != "Test Org" {
		t.Fatalf("expected name Test Org, got %s", org.Name)
	}

	// Not found
	org, err = repo.GetOrganization(ctx, 99999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if org != nil {
		t.Fatal("expected nil")
	}
}

func TestPostgresRepository_UpdateAPIKeyLastUsed(t *testing.T) {
	repo, pool := setupAPIGatewayRepo(t)
	ctx := context.Background()

	var orgID int64
	pool.QueryRow(ctx, `INSERT INTO organizations (name, email, status) VALUES ('Test Org', 'test3@example.com', 'active') RETURNING id`).Scan(&orgID)

	var keyID int64
	pool.QueryRow(ctx, `INSERT INTO api_keys (org_id, name, key_hash, status, last_used_at) VALUES ($1, 'Key', 'hash', 'active', NULL) RETURNING id`, orgID).Scan(&keyID)

	err := repo.UpdateAPIKeyLastUsed(ctx, keyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var lastUsed *time.Time
	pool.QueryRow(ctx, `SELECT last_used_at FROM api_keys WHERE id = $1`, keyID).Scan(&lastUsed)
	if lastUsed == nil {
		t.Fatal("expected last_used_at to be set")
	}
	if time.Since(*lastUsed) > time.Minute {
		t.Fatal("expected recent last_used_at")
	}
}
