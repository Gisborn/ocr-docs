package models

import (
	"time"
)

// Organization представляет организацию-клиента
type Organization struct {
	ID                int64      `json:"id" db:"id"`
	Name              string     `json:"name" db:"name"`
	Email             string     `json:"email" db:"email"`
	EmailVerified     bool       `json:"email_verified" db:"email_verified"`
	PasswordHash      string     `json:"-" db:"password_hash"`
	BillingAccountID  *int64     `json:"billing_account_id,omitempty" db:"billing_account_id"`
	Status            string     `json:"status" db:"status"` // active, blocked, archived
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
}

// User представляет пользователя личного кабинета
type User struct {
	ID           int64     `json:"id" db:"id"`
	OrgID        int64     `json:"org_id" db:"org_id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Role         string    `json:"role" db:"role"` // admin, user
	LastLoginAt  *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// APIKey представляет API ключ (для отображения в ЛК)
type APIKey struct {
	ID         int64      `json:"id" db:"id"`
	OrgID      int64      `json:"org_id" db:"org_id"`
	Name       string     `json:"name" db:"name"`
	KeyPreview string     `json:"key_preview" db:"-"` // последние 4 символа
	KeyHash    string     `json:"-" db:"key_hash"`
	Status     string     `json:"status" db:"status"` // active, revoked
	RateLimitRPS int      `json:"rate_limit_rps" db:"rate_limit_rps"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
}

// AccountEvent представляет событие аккаунта (audit log)
type AccountEvent struct {
	ID        int64          `json:"id" db:"id"`
	OrgID     int64          `json:"org_id" db:"org_id"`
	EventType string         `json:"event_type" db:"event_type"`
	Payload   map[string]interface{} `json:"payload" db:"payload"`
	ActorID   *int64         `json:"actor_id,omitempty" db:"actor_id"`
	CreatedAt time.Time      `json:"created_at" db:"created_at"`
}

// Session представляет сессию пользователя
type Session struct {
	ID               string    `json:"id" db:"id"`
	UserID           int64     `json:"user_id" db:"user_id"`
	OrgID            int64     `json:"org_id" db:"org_id"`
	BillingAccountID int64     `json:"billing_account_id" db:"billing_account_id"`
	Token            string    `json:"-" db:"token"`
	ExpiresAt        time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
}

// Valid проверяет, активна ли организация
func (o *Organization) Valid() bool {
	return o.Status == "active" && o.EmailVerified
}

// IsAdmin проверяет, является ли пользователь администратором
func (u *User) IsAdmin() bool {
	return u.Role == "admin"
}

// Valid проверяет, активен ли API ключ
func (k *APIKey) Valid() bool {
	return k.Status == "active"
}
