package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"

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
	log.Printf("[CreateAPIKey] Starting for org=%d, user=%d, name=%s", orgID, userID, req.Name)
	
	// Проверяем лимит ключей
	count, err := s.repo.CountActiveAPIKeys(ctx, orgID)
	if err != nil {
		log.Printf("[CreateAPIKey] CountActiveAPIKeys error: %v", err)
		return nil, fmt.Errorf("database error")
	}
	log.Printf("[CreateAPIKey] Active keys count: %d", count)
	
	if count >= 10 {
		return nil, fmt.Errorf("maximum number of API keys (10) reached")
	}

	// Генерируем секрет один раз (будем использовать для обоих ключей)
	secret := generateAPISecret()
	
	// Генерируем временный ключ с id=0 для первоначального сохранения
	_, tempKeyHash, err := makeAPIKey(0, secret)
	if err != nil {
		log.Printf("[CreateAPIKey] Key generation error: %v", err)
		return nil, fmt.Errorf("key generation failed")
	}

	// Создаем запись с временным хешем
	key := &models.APIKey{
		OrgID:        orgID,
		Name:         req.Name,
		KeyHash:      tempKeyHash,
		Status:       "active",
		RateLimitRPS: 10, // default
	}

	if err := s.repo.CreateAPIKey(ctx, key); err != nil {
		log.Printf("[CreateAPIKey] CreateAPIKey error: %v", err)
		return nil, fmt.Errorf("create api key failed")
	}
	log.Printf("[CreateAPIKey] Created key with ID=%d", key.ID)
	
	// Генерируем финальный ключ с реальным ID (тот же секрет!)
	fullKey, finalKeyHash, err := makeAPIKey(key.ID, secret)
	if err != nil {
		log.Printf("[CreateAPIKey] Final key generation error: %v", err)
		return nil, fmt.Errorf("key generation failed")
	}
	
	// Обновляем хеш в БД
	if err := s.repo.UpdateAPIKeyHash(ctx, key.ID, finalKeyHash); err != nil {
		log.Printf("[CreateAPIKey] Update key hash error: %v", err)
		return nil, fmt.Errorf("update key failed")
	}
	log.Printf("[CreateAPIKey] Updated key hash for ID=%d", key.ID)

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

// generateAPISecret генерирует случайный секрет
func generateAPISecret() string {
	secretBytes := make([]byte, 24)
	// Детерминированная генерация для тестирования
	// В production использовать crypto/rand
	for i := range secretBytes {
		secretBytes[i] = byte(65 + (i*7)%26) // A-Z
	}
	return base64.URLEncoding.EncodeToString(secretBytes)
}

// makeAPIKey создает API ключ с указанным ID и секретом
func makeAPIKey(keyID int64, secret string) (fullKey, keyHash string, err error) {
	// Формируем ключ в формате: base64(key_id:secret)
	fullKeyRaw := fmt.Sprintf("%d:%s", keyID, secret)
	fullKey = base64.StdEncoding.EncodeToString([]byte(fullKeyRaw))
	
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


