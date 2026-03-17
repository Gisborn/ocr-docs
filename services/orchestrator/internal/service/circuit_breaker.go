package service

import (
	"sync"
	"time"
)

// CircuitBreaker реализует паттерн Circuit Breaker для OCR провайдеров
type CircuitBreaker struct {
	name                string
	failureThreshold    int           // Количество ошибок для открытия
	successThreshold    int           // Количество успехов для закрытия
	timeout             time.Duration // Время на которое открывается
	failureCount        int
	successCount        int
	lastFailureTime     time.Time
	state               State
	mutex               sync.RWMutex
}

// State состояние Circuit Breaker
type State int

const (
	StateClosed    State = iota // Нормальная работа
	StateOpen                   // Ошибки, запросы не идут
	StateHalfOpen               // Пробуем восстановление
)

// NewCircuitBreaker создает новый Circuit Breaker
// failureThreshold: количество ошибок подряд для открытия (по умолчанию 5)
// timeout: время на которое открывается (по умолчанию 30 сек)
func NewCircuitBreaker(name string, failureThreshold int, timeout time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &CircuitBreaker{
		name:             name,
		failureThreshold: failureThreshold,
		successThreshold: 2, // Для half-open достаточно 2 успехов
		timeout:          timeout,
		state:            StateClosed,
	}
}

// Allow проверяет можно ли выполнять запрос
func (cb *CircuitBreaker) Allow() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Проверяем не истек ли таймаут
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.mutex.RUnlock()
			cb.mutex.Lock()
			cb.state = StateHalfOpen
			cb.mutex.Unlock()
			cb.mutex.RLock()
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return true
	}
}

// RecordSuccess записывает успешный вызов
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failureCount = 0
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.state = StateClosed
			cb.failureCount = 0
			cb.successCount = 0
		}
	}
}

// RecordFailure записывает ошибку
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
		}
	case StateHalfOpen:
		cb.state = StateOpen
		cb.failureCount++
		cb.successCount = 0
	}
}

// State возвращает текущее состояние
func (cb *CircuitBreaker) State() State {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// Stats возвращает статистику для мониторинга
func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return map[string]interface{}{
		"name":           cb.name,
		"state":          cb.state.String(),
		"failure_count":  cb.failureCount,
		"success_count":  cb.successCount,
		"last_failure":   cb.lastFailureTime,
	}
}

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}
