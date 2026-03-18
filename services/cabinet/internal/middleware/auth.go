package middleware

import (
	"context"
	"net/http"
	"strings"

	"scan.passport.local/api/services/cabinet/internal/repository"
)

// AuthMiddleware middleware для проверки сессии
type AuthMiddleware struct {
	repo repository.Repository
}

// NewAuthMiddleware создает middleware
func NewAuthMiddleware(repo repository.Repository) *AuthMiddleware {
	return &AuthMiddleware{repo: repo}
}

// contextKey тип для ключей контекста
type contextKey string

const (
	ContextKeyUserID contextKey = "user_id"
	ContextKeyOrgID  contextKey = "org_id"
	ContextKeyRole   contextKey = "role"
)

// Handler проверяет сессию
func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Получаем токен из заголовка или cookie
		token := extractToken(r)
		if token == "" {
			http.Error(w, `{"error":"unauthorized","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Проверяем сессию
		session, err := m.repo.GetSessionByToken(r.Context(), token)
		if err != nil || session == nil {
			http.Error(w, `{"error":"unauthorized","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		// Добавляем в контекст
		ctx := r.Context()
		ctx = context.WithValue(ctx, ContextKeyUserID, session.UserID)
		ctx = context.WithValue(ctx, ContextKeyOrgID, session.OrgID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalHandler проверяет сессию, но не требует её (для публичных страниц)
func (m *AuthMiddleware) OptionalHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token != "" {
			session, err := m.repo.GetSessionByToken(r.Context(), token)
			if err == nil && session != nil {
				ctx := r.Context()
				ctx = context.WithValue(ctx, ContextKeyUserID, session.UserID)
				ctx = context.WithValue(ctx, ContextKeyOrgID, session.OrgID)
				r = r.WithContext(ctx)
			}
		}

		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	// Пробуем заголовок Authorization
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Пробуем cookie
	if cookie, err := r.Cookie("session"); err == nil {
		return cookie.Value
	}

	// Пробуем query параметр (для разработки)
	return r.URL.Query().Get("token")
}

// GetUserID возвращает ID пользователя из контекста
func GetUserID(ctx context.Context) int64 {
	if id, ok := ctx.Value(ContextKeyUserID).(int64); ok {
		return id
	}
	return 0
}

// GetOrgID возвращает ID организации из контекста
func GetOrgID(ctx context.Context) int64 {
	if id, ok := ctx.Value(ContextKeyOrgID).(int64); ok {
		return id
	}
	return 0
}
