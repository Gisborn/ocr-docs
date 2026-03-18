package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter интерфейс для rate limiting
type RateLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error)
}

// RedisRateLimiter реализация на Redis
type RedisRateLimiter struct {
	client *redis.Client
}

// NewRedisRateLimiter создает rate limiter на Redis
func NewRedisRateLimiter(redisAddr string) *RedisRateLimiter {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0, // используем дефолтную БД
	})
	return &RedisRateLimiter{client: client}
}

// Allow проверяет, разрешено ли выполнить запрос
// Использует sliding window algorithm
func (r *RedisRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
	now := time.Now().Unix()
	windowStart := now - int64(window.Seconds())

	pipe := r.client.Pipeline()
	
	// Удаляем старые записи (вне окна)
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(windowStart, 10))
	
	// Получаем текущее количество запросов
	countCmd := pipe.ZCard(ctx, key)
	
	// Добавляем текущий запрос
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(now),
		Member: now,
	})
	
	// Устанавливаем TTL на ключ
	pipe.Expire(ctx, key, window)
	
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("redis pipeline: %w", err)
	}

	count := int(countCmd.Val())
	
	// +1 для текущего запроса
	if count+1 > limit {
		return false, limit - count, nil
	}
	
	return true, limit - count - 1, nil
}

// Close закрывает соединение с Redis
func (r *RedisRateLimiter) Close() error {
	return r.client.Close()
}

// RateLimitMiddleware создает middleware для rate limiting
type RateLimitMiddleware struct {
	limiter RateLimiter
	defaultRPS int
}

// NewRateLimitMiddleware создает middleware
func NewRateLimitMiddleware(limiter RateLimiter, defaultRPS int) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		limiter: limiter,
		defaultRPS: defaultRPS,
	}
}

// Handler возвращает http.Handler с rate limiting
func (m *RateLimitMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Получаем API ключ из контекста (установлен auth middleware)
		apiKeyID, ok := r.Context().Value("api_key_id").(int64)
		if !ok {
			// Если нет ключа в контексте, пропускаем (для публичных эндпоинтов)
			next.ServeHTTP(w, r)
			return
		}

		rateLimit, ok := r.Context().Value("rate_limit_rps").(int)
		if !ok || rateLimit <= 0 {
			rateLimit = m.defaultRPS
		}

		// Ключ для Redis: rate_limit:{api_key_id}
		redisKey := fmt.Sprintf("rate_limit:%d", apiKeyID)
		
		allowed, remaining, err := m.limiter.Allow(r.Context(), redisKey, rateLimit, time.Second)
		if err != nil {
			// При ошибке Redis пропускаем запрос, но логируем
			fmt.Printf("Rate limiter error: %v\n", err)
			next.ServeHTTP(w, r)
			return
		}

		// Устанавливаем заголовки RateLimit
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rateLimit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if !allowed {
			http.Error(w, `{"error":"rate limit exceeded","retry_after":1}`, http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
