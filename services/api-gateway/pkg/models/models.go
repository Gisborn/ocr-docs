package models

import (
	"time"
)

// APIKey представляет API ключ клиента
type APIKey struct {
	ID             int64      `json:"id" db:"id"`
	OrganizationID int64      `json:"organization_id" db:"org_id"`
	KeyHash        string     `json:"-" db:"key_hash"` // bcrypt hash, never expose
	Name           string     `json:"name" db:"name"`
	Status         string     `json:"status" db:"status"` // active, revoked
	RateLimitRPS   int        `json:"rate_limit_rps" db:"rate_limit_rps"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
}

// Organization представляет организацию-клиента
type Organization struct {
	ID               int64     `json:"id" db:"id"`
	Name             string    `json:"name" db:"name"`
	Status           string    `json:"status" db:"status"` // active, blocked, archived
	BillingAccountID *int64    `json:"billing_account_id,omitempty" db:"billing_account_id"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// Valid checks if API key is valid and not expired
func (k *APIKey) Valid() bool {
	if k.Status != "active" {
		return false
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		return false
	}
	return true
}

// Valid checks if organization is active
func (o *Organization) Valid() bool {
	return o.Status == "active"
}
