package service

import (
	"sync"
	"time"
)

// CircuitBreaker реализует паттерн Circuit Breaker
type CircuitBreaker struct {
	name             string
	failureThreshold int
	timeout          time.Duration
	
	failures   int
	lastFailure time.Time
	state      State
	mu         sync.RWMutex
}

// State состояние Circuit Breaker
type State int

const (
	StateClosed    State = iota // Нормальное состояние, запросы проходят
	StateOpen                   // Состояние ошибки, запросы блокируются
	StateHalfOpen               // Проверка восстановления
)

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

// NewCircuitBreaker создает новый Circuit Breaker
func NewCircuitBreaker(name string, failureThreshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		failureThreshold: failureThreshold,
		timeout:          timeout,
		state:            StateClosed,
	}
}

// Allow проверяет, можно ли выполнить запрос
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	
	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Проверяем, прошло ли время timeout
		if time.Since(cb.lastFailure) > cb.timeout {
			// Переходим в half-open
			cb.mu.RUnlock()
			cb.mu.Lock()
			cb.state = StateHalfOpen
			cb.mu.Unlock()
			cb.mu.RLock()
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return true
	}
}

// RecordSuccess фиксирует успешный запрос
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	switch cb.state {
	case StateHalfOpen:
		// Восстановление работы
		cb.state = StateClosed
		cb.failures = 0
	case StateClosed:
		// Сбрасываем счетчик ошибок
		cb.failures = 0
	}
}

// RecordFailure фиксирует ошибку
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	cb.failures++
	cb.lastFailure = time.Now()
	
	switch cb.state {
	case StateHalfOpen:
		// Снова открываем
		cb.state = StateOpen
	case StateClosed:
		if cb.failures >= cb.failureThreshold {
			cb.state = StateOpen
		}
	}
}

// State возвращает текущее состояние
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats возвращает статистику
func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	
	return map[string]interface{}{
		"name":             cb.name,
		"state":            cb.state.String(),
		"failures":         cb.failures,
		"failureThreshold": cb.failureThreshold,
		"timeout":          cb.timeout.String(),
	}
}
