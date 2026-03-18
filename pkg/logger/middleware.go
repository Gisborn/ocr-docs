package logger

import (
	"log"
	"net/http"
	"time"
)

// responseWriter обертка для захвата статуса ответа
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.size += n
	return n, err
}

// LoggingMiddleware логирует все HTTP запросы
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Оборачиваем ResponseWriter для отслеживания статуса
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		// Выполняем запрос
		next.ServeHTTP(wrapped, r)
		
		// Логируем
		duration := time.Since(start)
		
		log.Printf("[%s] %s %s - %d (%d bytes) - %v - %s",
			time.Now().Format("2006-01-02 15:04:05"),
			r.Method,
			r.URL.Path,
			wrapped.statusCode,
			wrapped.size,
			duration,
			r.RemoteAddr,
		)
	})
}
