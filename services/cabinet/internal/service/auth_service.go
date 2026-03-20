package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"scan.passport.local/api/services/cabinet/internal/repository"
	"scan.passport.local/api/services/cabinet/pkg/models"
	"golang.org/x/crypto/bcrypt"
)

// AuthService сервис аутентификации
type AuthService struct {
	repo         repository.Repository
	billingURL   string
	billingToken string
}

// NewAuthService создает сервис аутентификации
func NewAuthService(repo repository.Repository, billingURL, billingToken string) *AuthService {
	return &AuthService{
		repo:         repo,
		billingURL:   billingURL,
		billingToken: billingToken,
	}
}

// RegisterRequest запрос на регистрацию
type RegisterRequest struct {
	OrganizationName string `json:"organization_name"`
	Email            string `json:"email"`
	Password         string `json:"password"`
}

// RegisterResponse ответ на регистрацию
type RegisterResponse struct {
	OrgID  int64  `json:"org_id"`
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
}

// Register регистрирует новую организацию
func (s *AuthService) Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	// Валидация
	if req.OrganizationName == "" {
		return nil, fmt.Errorf("organization name is required")
	}
	if req.Email == "" || !isValidEmail(req.Email) {
		return nil, fmt.Errorf("valid email is required")
	}
	if len(req.Password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	// Проверяем, не занят ли email
	existing, err := s.repo.GetOrganizationByEmail(ctx, strings.ToLower(req.Email))
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("email already registered")
	}

	// Хешируем пароль
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("password hash error: %w", err)
	}

	// Создаем организацию
	org := &models.Organization{
		Name:         req.OrganizationName,
		Email:        strings.ToLower(req.Email),
		PasswordHash: string(passwordHash),
		Status:       "pending", // Ожидает подтверждения email
	}

	if err := s.repo.CreateOrganization(ctx, org); err != nil {
		return nil, fmt.Errorf("create organization failed: %w", err)
	}

	// Создаем пользователя (администратор)
	user := &models.User{
		OrgID:        org.ID,
		Email:        strings.ToLower(req.Email),
		PasswordHash: string(passwordHash),
		Role:         "admin",
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("create user failed: %w", err)
	}

	// Логируем событие
	s.repo.CreateAccountEvent(ctx, &models.AccountEvent{
		OrgID:     org.ID,
		EventType: "organization_registered",
		Payload: map[string]interface{}{
			"email": req.Email,
		},
		ActorID: &user.ID,
	})

	// TODO: Отправить письмо с подтверждением email

	return &RegisterResponse{
		OrgID:  org.ID,
		UserID: user.ID,
		Email:  req.Email,
	}, nil
}

// LoginRequest запрос на вход
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse ответ на вход
type LoginResponse struct {
	SessionToken string `json:"session_token"`
	ExpiresAt    string `json:"expires_at"`
	User         *UserInfo `json:"user"`
}

// UserInfo информация о пользователе
type UserInfo struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Login выполняет вход
func (s *AuthService) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	log.Printf("[Login] Attempt for email: %s", req.Email)
	
	// Находим организацию по email
	org, err := s.repo.GetOrganizationByEmail(ctx, strings.ToLower(req.Email))
	if err != nil {
		log.Printf("[Login] Database error: %v", err)
		return nil, fmt.Errorf("database error")
	}
	if org == nil {
		log.Printf("[Login] Organization not found for email: %s", req.Email)
		return nil, fmt.Errorf("invalid credentials")
	}
	
	log.Printf("[Login] Found organization ID: %d, status: %s, email_verified: %v", 
		org.ID, org.Status, org.EmailVerified)

	// Проверяем статус
	if org.Status != "active" {
		log.Printf("[Login] Account not active: %s", org.Status)
		return nil, fmt.Errorf("account not active")
	}
	if !org.EmailVerified {
		log.Printf("[Login] Email not verified")
		return nil, fmt.Errorf("email not verified")
	}

	// Находим пользователя
	user, err := s.repo.GetUserByEmail(ctx, org.ID, strings.ToLower(req.Email))
	if err != nil {
		log.Printf("[Login] Database error finding user: %v", err)
		return nil, fmt.Errorf("database error")
	}
	if user == nil {
		log.Printf("[Login] User not found in org %d", org.ID)
		return nil, fmt.Errorf("invalid credentials")
	}

	// Проверяем пароль
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		log.Printf("[Login] Invalid password for user ID: %d", user.ID)
		return nil, fmt.Errorf("invalid credentials")
	}
	
	log.Printf("[Login] Password verified for user ID: %d", user.ID)

	// Обновляем время входа
	s.repo.UpdateLastLogin(ctx, user.ID)

	// Создаем сессию
	sessionToken, err := generateSessionToken()
	if err != nil {
		log.Printf("[Login] Failed to generate session token: %v", err)
		return nil, fmt.Errorf("session creation failed")
	}

	// Получаем billing_account_id из организации
	var billingAccountID int64
	if org.BillingAccountID != nil {
		billingAccountID = *org.BillingAccountID
	}
	
	session := &models.Session{
		ID:               generateID(),
		UserID:           user.ID,
		OrgID:            org.ID,
		BillingAccountID: billingAccountID,
		Token:            sessionToken,
		ExpiresAt:        time.Now().Add(24 * time.Hour),
	}

	if err := s.repo.CreateSession(ctx, session); err != nil {
		log.Printf("[Login] Failed to create session: %v", err)
		return nil, fmt.Errorf("session creation failed")
	}

	// Логируем событие
	s.repo.CreateAccountEvent(ctx, &models.AccountEvent{
		OrgID:     org.ID,
		EventType: "user_login",
		Payload:   map[string]interface{}{},
		ActorID:   &user.ID,
	})

	return &LoginResponse{
		SessionToken: sessionToken,
		ExpiresAt:    session.ExpiresAt.Format(time.RFC3339),
		User: &UserInfo{
			ID:    user.ID,
			Email: user.Email,
			Role:  user.Role,
		},
	}, nil
}

// Logout выполняет выход
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.repo.DeleteSession(ctx, token)
}

// GetSession возвращает сессию по токену
func (s *AuthService) GetSession(ctx context.Context, token string) (*models.Session, error) {
	return s.repo.GetSessionByToken(ctx, token)
}

// VerifyEmail подтверждает email
func (s *AuthService) VerifyEmail(ctx context.Context, token string) error {
	// TODO: Реализовать проверку токена подтверждения
	// Сейчас заглушка - просто активируем организацию
	return nil
}

// CreateBillingAccount создает billing account через API Billing Service
func (s *AuthService) CreateBillingAccount(ctx context.Context, orgID int64) (int64, error) {
	// HTTP запрос к Billing Service
	url := fmt.Sprintf("%s/accounts", s.billingURL)
	
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return 0, err
	}
	
	req.Header.Set("Authorization", "Bearer "+s.billingToken)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("billing service unavailable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("billing service error: %d", resp.StatusCode)
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	// Сохраняем связь
	if err := s.repo.SetBillingAccountID(ctx, orgID, result.ID); err != nil {
		return 0, err
	}

	return result.ID, nil
}

// GetOrgByID получает организацию по ID
func (s *AuthService) GetOrgByID(ctx context.Context, id int64) (*models.Organization, error) {
	return s.repo.GetOrganizationByID(ctx, id)
}

// Вспомогательные функции

func isValidEmail(email string) bool {
	// Простая проверка
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}

func generateSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

func generateID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)
}
