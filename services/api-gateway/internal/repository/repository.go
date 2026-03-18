package repository

import (
	"context"
	"fmt"

	"github.com/api-scan/api-scan/services/api-gateway/pkg/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository интерфейс для работы с данными
type Repository interface {
	GetAPIKeyByID(ctx context.Context, id int64) (*models.APIKey, error)
	GetOrganization(ctx context.Context, id int64) (*models.Organization, error)
	UpdateAPIKeyLastUsed(ctx context.Context, keyID int64) error
}

// PostgresRepository реализация на PostgreSQL
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository создает новый репозиторий
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// GetAPIKeyByID находит API ключ по ID
func (r *PostgresRepository) GetAPIKeyByID(ctx context.Context, id int64) (*models.APIKey, error) {
	key := &models.APIKey{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, key_hash, name, status, rate_limit_rps, created_at, expires_at, last_used_at
		 FROM api_keys WHERE id = $1`,
		id,
	).Scan(&key.ID, &key.OrganizationID, &key.KeyHash, &key.Name, &key.Status,
		&key.RateLimitRPS, &key.CreatedAt, &key.ExpiresAt, &key.LastUsedAt)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return key, nil
}

// GetOrganization получает организацию по ID
func (r *PostgresRepository) GetOrganization(ctx context.Context, id int64) (*models.Organization, error) {
	org := &models.Organization{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, status, created_at, updated_at FROM organizations WHERE id = $1`,
		id,
	).Scan(&org.ID, &org.Name, &org.Status, &org.CreatedAt, &org.UpdatedAt)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return org, nil
}

// UpdateAPIKeyLastUsed обновляет время последнего использования
func (r *PostgresRepository) UpdateAPIKeyLastUsed(ctx context.Context, keyID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`,
		keyID,
	)
	return err
}
