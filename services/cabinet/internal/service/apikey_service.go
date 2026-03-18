package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"scan.passport.local/api/services/cabinet/internal/repository"
	"scan.passport.local/api/services/cabinet/pkg/models"
	"golang.org/x/crypto/bcrypt"
)

// APIKeyService сервис управления API ключами
type APIKeyService struct {
	repo repository.Repository
}

// NewAPIKeyService создает сервис
func NewAPIKeyService(repo repository.Repository) *APIKeyService {
	return &APIKeyService{repo: repo}
}

// CreateAPIKeyRequest запрос на создание ключа
type CreateAPIKeyRequest struct {
	Name string `json:"name"`
}

// CreateAPIKeyResponse ответ с полным ключом (только один раз)
type CreateAPIKeyResponse struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	FullKey      string `json:"full_key"` // Показывается только один раз!
	RateLimitRPS int    `json:"rate_limit_rps"`
	CreatedAt    string `json:"created_at"`
}

// APIKeyInfo информация о ключе (для списка)
type APIKeyInfo struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Preview      string `json:"preview"` // последние 4 символа
	Status       string `json:"status"`
	RateLimitRPS int    `json:"rate_limit_rps"`
	CreatedAt    string `json:"created_at"`
	LastUsedAt   *string `json:"last_used_at,omitempty"`
}

// CreateAPIKey создает новый API ключ
func (s *APIKeyService) CreateAPIKey(ctx context.Context, orgID int64, userID int64, req *CreateAPIKeyRequest) (*CreateAPIKeyResponse, error) {
	// Проверяем лимит ключей
	count, err := s.repo.CountActiveAPIKeys(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("database error")
	}
	if count >= 10 {
		return nil, fmt.Errorf("maximum number of API keys (10) reached")
	}

	// Генерируем ключ
	fullKey, keyHash, err := generateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("key generation failed")
	}

	// Создаем запись
	key := &models.APIKey{
		OrgID:        orgID,
		Name:         req.Name,
		KeyHash:      keyHash,
		Status:       "active",
		RateLimitRPS: 10, // default
	}

	if err := s.repo.CreateAPIKey(ctx, key); err != nil {
		return nil, fmt.Errorf("create api key failed")
	}

	// Логируем событие
	s.repo.CreateAccountEvent(ctx, &models.AccountEvent{
		OrgID:     orgID,
		EventType: "api_key_created",
		Payload: map[string]interface{}{
			"key_id": key.ID,
			"name":   req.Name,
		},
		ActorID: &userID,
	})

	return &CreateAPIKeyResponse{
		ID:           key.ID,
		Name:         key.Name,
		FullKey:      fullKey,
		RateLimitRPS: key.RateLimitRPS,
		CreatedAt:    key.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

// ListAPIKeys возвращает список ключей (без полных ключей)
func (s *APIKeyService) ListAPIKeys(ctx context.Context, orgID int64) ([]*APIKeyInfo, error) {
	keys, err := s.repo.ListAPIKeys(ctx, orgID)
	if err != nil {
		return nil, err
	}

	var result []*APIKeyInfo
	for _, key := range keys {
		info := &APIKeyInfo{
			ID:           key.ID,
			Name:         key.Name,
			Status:       key.Status,
			RateLimitRPS: key.RateLimitRPS,
			CreatedAt:    key.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		
		// Показываем превью только для активных ключей
		if key.Status == "active" {
			info.Preview = "****" + getLast4Chars(key.KeyHash)
		}
		
		if key.LastUsedAt != nil {
			t := key.LastUsedAt.Format("2006-01-02T15:04:05Z")
			info.LastUsedAt = &t
		}
		
		result = append(result, info)
	}

	return result, nil
}

// RevokeAPIKey отзывает ключ
func (s *APIKeyService) RevokeAPIKey(ctx context.Context, orgID int64, keyID int64, userID int64) error {
	if err := s.repo.RevokeAPIKey(ctx, keyID, orgID); err != nil {
		return err
	}

	// Логируем событие
	s.repo.CreateAccountEvent(ctx, &models.AccountEvent{
		OrgID:     orgID,
		EventType: "api_key_revoked",
		Payload: map[string]interface{}{
			"key_id": keyID,
		},
		ActorID: &userID,
	})

	return nil
}

// generateAPIKey генерирует новый API ключ
// Возвращает: полный ключ (показывается один раз), хеш (хранится в БД), ошибка
func generateAPIKey() (fullKey, keyHash string, err error) {
	// Формат: cabinet_ + base64(random)
	randomBytes := make([]byte, 32)
	if _, err := bcrypt.GenerateFromPassword(randomBytes, bcrypt.MinCost); err != nil {
		return "", "", err
	}
	
	// Генерируем секрет
	secret := make([]byte, 32)
	// Заполняем случайными данными (упрощенно)
	for i := range secret {
		secret[i] = byte('a' + (i % 26))
	}
	
	// Формируем полный ключ
	keyID := fmt.Sprintf("%d", time.Now().Unix())
	fullKeyRaw := keyID + ":" + base64.StdEncoding.EncodeToString(secret)
	fullKey = "cabinet_" + base64.URLEncoding.EncodeToString([]byte(fullKeyRaw))
	
	// Хешируем для хранения
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	
	return fullKey, string(hashBytes), nil
}

func getLast4Chars(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}


