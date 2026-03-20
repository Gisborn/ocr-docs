package repository

import (
	"context"
	"fmt"
	"time"

	"scan.passport.local/api/services/cabinet/pkg/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository интерфейс для работы с данными
type Repository interface {
	// Organizations
	CreateOrganization(ctx context.Context, org *models.Organization) error
	GetOrganizationByEmail(ctx context.Context, email string) (*models.Organization, error)
	GetOrganizationByID(ctx context.Context, id int64) (*models.Organization, error)
	UpdateOrganization(ctx context.Context, org *models.Organization) error
	SetBillingAccountID(ctx context.Context, orgID int64, billingAccountID int64) error
	
	// Users
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByEmail(ctx context.Context, orgID int64, email string) (*models.User, error)
	GetUserByID(ctx context.Context, id int64) (*models.User, error)
	UpdateUser(ctx context.Context, user *models.User) error
	UpdateLastLogin(ctx context.Context, userID int64) error
	
	// API Keys
	CreateAPIKey(ctx context.Context, key *models.APIKey) error
	GetAPIKeyByID(ctx context.Context, id int64) (*models.APIKey, error)
	ListAPIKeys(ctx context.Context, orgID int64) ([]*models.APIKey, error)
	RevokeAPIKey(ctx context.Context, id int64, orgID int64) error
	CountActiveAPIKeys(ctx context.Context, orgID int64) (int, error)
	UpdateAPIKeyHash(ctx context.Context, keyID int64, keyHash string) error
	
	// Sessions
	CreateSession(ctx context.Context, session *models.Session) error
	GetSessionByToken(ctx context.Context, token string) (*models.Session, error)
	DeleteSession(ctx context.Context, token string) error
	
	// Account Events
	CreateAccountEvent(ctx context.Context, event *models.AccountEvent) error
	ListAccountEvents(ctx context.Context, orgID int64, eventType string, from, to time.Time, limit, offset int) ([]*models.AccountEvent, error)
}

// PostgresRepository реализация на PostgreSQL
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository создает новый репозиторий
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// CreateOrganization создает новую организацию
func (r *PostgresRepository) CreateOrganization(ctx context.Context, org *models.Organization) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO organizations (name, email, password_hash, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), NOW())
		 RETURNING id, created_at, updated_at`,
		org.Name, org.Email, org.PasswordHash, org.Status,
	).Scan(&org.ID, &org.CreatedAt, &org.UpdatedAt)
}

// GetOrganizationByEmail находит организацию по email
func (r *PostgresRepository) GetOrganizationByEmail(ctx context.Context, email string) (*models.Organization, error) {
	org := &models.Organization{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, email, email_verified, password_hash, billing_account_id, status, created_at, updated_at
		 FROM organizations WHERE email = $1`,
		email,
	).Scan(&org.ID, &org.Name, &org.Email, &org.EmailVerified, &org.PasswordHash,
		&org.BillingAccountID, &org.Status, &org.CreatedAt, &org.UpdatedAt)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return org, nil
}

// GetOrganizationByID находит организацию по ID
func (r *PostgresRepository) GetOrganizationByID(ctx context.Context, id int64) (*models.Organization, error) {
	org := &models.Organization{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, email, email_verified, password_hash, billing_account_id, status, created_at, updated_at
		 FROM organizations WHERE id = $1`,
		id,
	).Scan(&org.ID, &org.Name, &org.Email, &org.EmailVerified, &org.PasswordHash,
		&org.BillingAccountID, &org.Status, &org.CreatedAt, &org.UpdatedAt)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return org, nil
}

// UpdateOrganization обновляет организацию
func (r *PostgresRepository) UpdateOrganization(ctx context.Context, org *models.Organization) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE organizations SET name = $1, email_verified = $2, status = $3, updated_at = NOW()
		 WHERE id = $4`,
		org.Name, org.EmailVerified, org.Status, org.ID,
	)
	return err
}

// SetBillingAccountID устанавливает billing account ID
func (r *PostgresRepository) SetBillingAccountID(ctx context.Context, orgID int64, billingAccountID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE organizations SET billing_account_id = $1, updated_at = NOW() WHERE id = $2`,
		billingAccountID, orgID,
	)
	return err
}

// CreateUser создает нового пользователя
func (r *PostgresRepository) CreateUser(ctx context.Context, user *models.User) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO users (org_id, email, password_hash, role, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), NOW())
		 RETURNING id, created_at, updated_at`,
		user.OrgID, user.Email, user.PasswordHash, user.Role,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

// GetUserByEmail находит пользователя по email
func (r *PostgresRepository) GetUserByEmail(ctx context.Context, orgID int64, email string) (*models.User, error) {
	user := &models.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, email, password_hash, role, last_login_at, created_at, updated_at
		 FROM users WHERE org_id = $1 AND email = $2`,
		orgID, email,
	).Scan(&user.ID, &user.OrgID, &user.Email, &user.PasswordHash, &user.Role,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return user, nil
}

// GetUserByID находит пользователя по ID
func (r *PostgresRepository) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	user := &models.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, email, password_hash, role, last_login_at, created_at, updated_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&user.ID, &user.OrgID, &user.Email, &user.PasswordHash, &user.Role,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return user, nil
}

// UpdateUser обновляет пользователя
func (r *PostgresRepository) UpdateUser(ctx context.Context, user *models.User) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET email = $1, role = $2, updated_at = NOW() WHERE id = $3`,
		user.Email, user.Role, user.ID,
	)
	return err
}

// UpdateLastLogin обновляет время последнего входа
func (r *PostgresRepository) UpdateLastLogin(ctx context.Context, userID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET last_login_at = NOW() WHERE id = $1`,
		userID,
	)
	return err
}

// CreateAPIKey создает новый API ключ
func (r *PostgresRepository) CreateAPIKey(ctx context.Context, key *models.APIKey) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO api_keys (org_id, name, key_hash, status, rate_limit_rps, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 RETURNING id, created_at`,
		key.OrgID, key.Name, key.KeyHash, key.Status, key.RateLimitRPS,
	).Scan(&key.ID, &key.CreatedAt)
}

// GetAPIKeyByID находит API ключ по ID
func (r *PostgresRepository) GetAPIKeyByID(ctx context.Context, id int64) (*models.APIKey, error) {
	key := &models.APIKey{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, name, key_hash, status, rate_limit_rps, created_at, last_used_at
		 FROM api_keys WHERE id = $1`,
		id,
	).Scan(&key.ID, &key.OrgID, &key.Name, &key.KeyHash, &key.Status,
		&key.RateLimitRPS, &key.CreatedAt, &key.LastUsedAt)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return key, nil
}

// ListAPIKeys возвращает список API ключей организации
func (r *PostgresRepository) ListAPIKeys(ctx context.Context, orgID int64) ([]*models.APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, name, key_hash, status, rate_limit_rps, created_at, last_used_at
		 FROM api_keys WHERE org_id = $1 ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*models.APIKey
	for rows.Next() {
		key := &models.APIKey{}
		err := rows.Scan(&key.ID, &key.OrgID, &key.Name, &key.KeyHash, &key.Status,
			&key.RateLimitRPS, &key.CreatedAt, &key.LastUsedAt)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// RevokeAPIKey отзывает API ключ
func (r *PostgresRepository) RevokeAPIKey(ctx context.Context, id int64, orgID int64) error {
	result, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET status = 'revoked', revoked_at = NOW()
		 WHERE id = $1 AND org_id = $2 AND status = 'active'`,
		id, orgID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("api key not found or already revoked")
	}
	return nil
}

// CountActiveAPIKeys считает активные ключи
func (r *PostgresRepository) CountActiveAPIKeys(ctx context.Context, orgID int64) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE org_id = $1 AND status = 'active'`,
		orgID,
	).Scan(&count)
	return count, err
}

// UpdateAPIKeyHash обновляет хеш ключа
func (r *PostgresRepository) UpdateAPIKeyHash(ctx context.Context, keyID int64, keyHash string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET key_hash = $1, updated_at = NOW() WHERE id = $2`,
		keyHash, keyID,
	)
	return err
}

// CreateSession создает сессию
func (r *PostgresRepository) CreateSession(ctx context.Context, session *models.Session) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO sessions (id, user_id, org_id, billing_account_id, token, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
		session.ID, session.UserID, session.OrgID, session.BillingAccountID, session.Token, session.ExpiresAt,
	)
	return err
}

// GetSessionByToken находит сессию по токену
func (r *PostgresRepository) GetSessionByToken(ctx context.Context, token string) (*models.Session, error) {
	session := &models.Session{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, org_id, billing_account_id, token, expires_at, created_at
		 FROM sessions WHERE token = $1 AND expires_at > NOW()`,
		token,
	).Scan(&session.ID, &session.UserID, &session.OrgID, &session.BillingAccountID, &session.Token, &session.ExpiresAt, &session.CreatedAt)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return session, nil
}

// DeleteSession удаляет сессию
func (r *PostgresRepository) DeleteSession(ctx context.Context, token string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

// CreateAccountEvent создает событие аккаунта
func (r *PostgresRepository) CreateAccountEvent(ctx context.Context, event *models.AccountEvent) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO account_events (org_id, event_type, payload, actor_id, created_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		event.OrgID, event.EventType, event.Payload, event.ActorID,
	)
	return err
}

// ListAccountEvents возвращает список событий
func (r *PostgresRepository) ListAccountEvents(ctx context.Context, orgID int64, eventType string, from, to time.Time, limit, offset int) ([]*models.AccountEvent, error) {
	query := `SELECT id, org_id, event_type, payload, actor_id, created_at
			  FROM account_events WHERE org_id = $1`
	args := []interface{}{orgID}
	argCount := 1

	if eventType != "" {
		argCount++
		query += fmt.Sprintf(" AND event_type = $%d", argCount)
		args = append(args, eventType)
	}

	if !from.IsZero() {
		argCount++
		query += fmt.Sprintf(" AND created_at >= $%d", argCount)
		args = append(args, from)
	}

	if !to.IsZero() {
		argCount++
		query += fmt.Sprintf(" AND created_at <= $%d", argCount)
		args = append(args, to)
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		argCount++
		query += fmt.Sprintf(" LIMIT $%d", argCount)
		args = append(args, limit)
	}

	if offset > 0 {
		argCount++
		query += fmt.Sprintf(" OFFSET $%d", argCount)
		args = append(args, offset)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*models.AccountEvent
	for rows.Next() {
		event := &models.AccountEvent{}
		err := rows.Scan(&event.ID, &event.OrgID, &event.EventType, &event.Payload, &event.ActorID, &event.CreatedAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
