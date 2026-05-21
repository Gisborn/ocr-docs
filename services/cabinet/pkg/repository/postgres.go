package repository

import (
	"github.com/jackc/pgx/v5/pgxpool"

	internal "scan.passport.local/api/services/cabinet/internal/repository"
)

// NewPostgresRepository creates a new PostgreSQL-backed repository.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return internal.NewPostgresRepository(pool)
}
