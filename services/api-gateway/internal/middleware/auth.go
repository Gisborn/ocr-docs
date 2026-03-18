package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/api-scan/api-scan/services/api-gateway/internal/repository"
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
			http.Error(w, `{"error":"missing api key","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Валидируем формат ключа
		keyID, secret, err := ParseAPIKey(apiKey)
		if err != nil {
			http.Error(w, `{"error":"invalid api key format","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Получаем ключ из БД
		key, err := m.repo.GetAPIKeyByID(r.Context(), keyID)
		if err != nil {
			http.Error(w, `{"error":"internal error","code":"INTERNAL_ERROR"}`, http.StatusInternalServerError)
			return
		}
		if key == nil {
			http.Error(w, `{"error":"invalid api key","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Проверяем статус ключа
		if !key.Valid() {
			http.Error(w, `{"error":"api key revoked or expired","code":"FORBIDDEN"}`, http.StatusForbidden)
			return
		}

		// Проверяем bcrypt хеш
		if err := bcrypt.CompareHashAndPassword([]byte(key.KeyHash), []byte(secret)); err != nil {
			http.Error(w, `{"error":"invalid api key","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Проверяем организацию
		org, err := m.repo.GetOrganization(r.Context(), key.OrganizationID)
		if err != nil || org == nil || !org.Valid() {
			http.Error(w, `{"error":"organization inactive or not found","code":"FORBIDDEN"}`, http.StatusForbidden)
			return
		}

		// Обновляем last_used_at (асинхронно)
		go m.repo.UpdateAPIKeyLastUsed(context.Background(), key.ID)

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
	// Генерируем хеш bcrypt
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("bcrypt hash: %w", err)
	}

	// Формируем полный ключ: base64(key_id:secret)
	fullKeyRaw := fmt.Sprintf("%d:%s", keyID, secret)
	fullKey = base64.StdEncoding.EncodeToString([]byte(fullKeyRaw))

	return fullKey, string(hashBytes), nil
}

// ExemptPath проверяет, exempt ли путь от аутентификации
func ExemptPath(path string) bool {
	exemptPaths := []string{
		"/health",
		"/webhooks/yookassa",
		"/v1/webhooks/",
	}

	for _, exempt := range exemptPaths {
		if strings.HasPrefix(path, exempt) {
			return true
		}
	}
	return false
}
