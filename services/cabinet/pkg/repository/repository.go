package repository

import (
	"context"
	"time"

	"scan.passport.local/api/services/cabinet/pkg/models"
)

// Repository defines storage operations for the cabinet service.
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
