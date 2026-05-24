package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"scan.passport.local/api/services/api-gateway/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

// AuthMiddleware middleware для аутентификации
type AuthMiddleware struct {
	repo repository.Repository
}

// NewAuthMiddleware создает auth middleware
func NewAuthMiddleware(repo repository.Repository) *AuthMiddleware {
	return &AuthMiddleware{repo: repo}
}

// contextKey тип для ключей контекста
type contextKey string

const (
	ContextKeyAPIKeyID       contextKey = "api_key_id"
	ContextKeyOrganizationID contextKey = "organization_id"
	ContextKeyRateLimitRPS   contextKey = "rate_limit_rps"
)

// Handler возвращает http.Handler с аутентификацией
func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Пропускаем exempt пути
		if ExemptPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Получаем API ключ из заголовка
		apiKey := r.Header.Get("X-Api-Key")
		if apiKey == "" {
			log.Printf("[Auth] Missing X-Api-Key header from %s", r.RemoteAddr)
			http.Error(w, `{"error":"missing api key","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Валидируем формат ключа
		keyID, _, err := ParseAPIKey(apiKey)
		if err != nil {
			log.Printf("[Auth] Invalid API key format from %s: %v", r.RemoteAddr, err)
			http.Error(w, `{"error":"invalid api key format","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Получаем ключ из БД
		key, err := m.repo.GetAPIKeyByID(r.Context(), keyID)
		if err != nil {
			log.Printf("[Auth] Database error getting key %d: %v", keyID, err)
			http.Error(w, `{"error":"internal error","code":"INTERNAL_ERROR"}`, http.StatusInternalServerError)
			return
		}
		if key == nil {
			log.Printf("[Auth] Key not found: %d from %s", keyID, r.RemoteAddr)
			http.Error(w, `{"error":"invalid api key","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Проверяем статус ключа
		if !key.Valid() {
			log.Printf("[Auth] Key %d is not valid (status: %s)", key.ID, key.Status)
			http.Error(w, `{"error":"api key revoked or expired","code":"FORBIDDEN"}`, http.StatusForbidden)
			return
		}

		// Проверяем bcrypt хеш
		// Сравниваем с полным ключом (base64), не только с секретом
		if err := bcrypt.CompareHashAndPassword([]byte(key.KeyHash), []byte(apiKey)); err != nil {
			log.Printf("[Auth] Invalid key hash for key %d from %s: %v", keyID, r.RemoteAddr, err)
			http.Error(w, `{"error":"invalid api key","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Проверяем организацию
		org, err := m.repo.GetOrganization(r.Context(), key.OrganizationID)
		if err != nil || org == nil || !org.Valid() {
			log.Printf("[Auth] Organization %d invalid or not found", key.OrganizationID)
			http.Error(w, `{"error":"organization inactive or not found","code":"FORBIDDEN"}`, http.StatusForbidden)
			return
		}

		// Обновляем last_used_at (асинхронно)
		go func() {
			_ = m.repo.UpdateAPIKeyLastUsed(context.Background(), key.ID)
		}()
		
		log.Printf("[Auth] Authenticated org=%d key=%d path=%s", org.ID, key.ID, r.URL.Path)

		// Добавляем данные в контекст
		ctx := r.Context()
		ctx = context.WithValue(ctx, ContextKeyAPIKeyID, key.ID)
		ctx = context.WithValue(ctx, ContextKeyOrganizationID, org.ID)
		ctx = context.WithValue(ctx, ContextKeyRateLimitRPS, key.RateLimitRPS)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ParseAPIKey парсит API ключ формата base64(key_id:secret)
// Возвращает keyID, secret, error
func ParseAPIKey(apiKey string) (int64, string, error) {
	// Декодируем base64
	decoded, err := base64.StdEncoding.DecodeString(apiKey)
	if err != nil {
		return 0, "", fmt.Errorf("invalid base64: %w", err)
	}

	// Разделяем по ":"
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid format")
	}

	// Парсим keyID
	keyID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid key id: %w", err)
	}

	return keyID, parts[1], nil
}

// GenerateAPIKey генерирует новый API ключ
// Возвращает полный ключ (для показа пользователю) и хеш (для хранения)
func GenerateAPIKey(keyID int64, secret string) (fullKey, hash string, err error) {
	// Формируем полный ключ: base64(key_id:secret)
	fullKeyRaw := fmt.Sprintf("%d:%s", keyID, secret)
	fullKey = base64.StdEncoding.EncodeToString([]byte(fullKeyRaw))

	// Генерируем хеш bcrypt от полного ключа (как в cabinet service)
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("bcrypt hash: %w", err)
	}

	return fullKey, string(hashBytes), nil
}

// ExemptPath проверяет, exempt ли путь от аутентификации
func ExemptPath(path string) bool {
	exemptPaths := []string{
		"/health",
		"/webhooks/yookassa",
		"/v1/webhooks/",
		"/api/v1/auth/",
		"/api/v1/payments/",
		"/api/v1/mock-payments",
		"/api/v1/api-keys",
		"/api/v1/balance",
	}

	for _, exempt := range exemptPaths {
		if strings.HasPrefix(path, exempt) {
			return true
		}
	}
	return false
}
