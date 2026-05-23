package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"scan.passport.local/api/services/billing/internal/repository"
	"scan.passport.local/api/services/billing/pkg/models"
)

// SubscriptionService сервис управления подписками
type SubscriptionService struct {
	repo repository.Repository
}

// NewSubscriptionService создает сервис подписок
func NewSubscriptionService(repo repository.Repository) *SubscriptionService {
	return &SubscriptionService{repo: repo}
}

// CreateSubscriptionRequest запрос на создание подписки
type CreateSubscriptionRequest struct {
	TariffCode    string `json:"tariff_code"`
	PaymentMethod string `json:"payment_method"` // "balance" или "card"
}

// CreateSubscriptionResponse ответ на создание подписки
type CreateSubscriptionResponse struct {
	SubscriptionID int32   `json:"subscription_id"`
	Status         string  `json:"status"`
	AmountCharged  float64 `json:"amount_charged_rub"`
}

// UpgradeSubscriptionRequest запрос на апгрейд подписки
type UpgradeSubscriptionRequest struct {
	TariffCode    string `json:"tariff_code"`
	PaymentMethod string `json:"payment_method"`
}

// UpgradeSubscriptionResponse ответ на апгрейд
type UpgradeSubscriptionResponse struct {
	SubscriptionID int32   `json:"subscription_id"`
	Status         string  `json:"status"`
	AmountCharged  float64 `json:"amount_charged_rub"`
}

// Balance информация о балансе
type Balance struct {
	AccountID         int64     `json:"account_id"`
	RealBalanceRub    float64   `json:"real_balance_rub"`
	PrepaidBalanceRub float64   `json:"prepaid_balance_rub"`
	ActiveTariff      *Tariff   `json:"active_tariff,omitempty"`
	CalculatedAt      time.Time `json:"calculated_at"`
}

// GetActiveSubscriptionResponse ответ с информацией о подписке
type GetActiveSubscriptionResponse struct {
	SubscriptionID   int32   `json:"subscription_id"`
	AccountID        int64   `json:"account_id"`
	TariffCode       string  `json:"tariff_code"`
	TariffName       string  `json:"tariff_name"`
	Status           string  `json:"status"`
	StartedAt        string  `json:"started_at"`
	ExpiresAt        string  `json:"expires_at"`
	AutoRenew        bool    `json:"auto_renew"`
	InitialPrepaidRub float64 `json:"initial_prepaid_rub"`
}

// Tariff информация о тарифе
type Tariff struct {
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	BasePriceRub  float64 `json:"base_price_rub"`
	PrepaidAmount float64 `json:"prepaid_amount_rub"`
}

// CreateSubscription создает подписку
func (s *SubscriptionService) CreateSubscription(ctx context.Context, accountID int64, req *CreateSubscriptionRequest) (*CreateSubscriptionResponse, error) {
	// Получаем тариф
	tariffVersion, err := s.repo.GetTariffVersionByCode(ctx, req.TariffCode)
	if err != nil {
		return nil, fmt.Errorf("tariff not found: %w", err)
	}

	tariff, err := s.repo.GetTariff(ctx, tariffVersion.TariffID)
	if err != nil {
		return nil, fmt.Errorf("tariff not found: %w", err)
	}

	// Валидация и default payment method
	if req.PaymentMethod == "" {
		req.PaymentMethod = "balance"
	}
	if req.PaymentMethod != "balance" && req.PaymentMethod != "card" {
		return nil, fmt.Errorf("invalid payment_method: %s", req.PaymentMethod)
	}

	// Проверяем баланс
	balance, err := s.GetBalance(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	if req.PaymentMethod == "balance" {
		if balance.RealBalanceRub < tariffVersion.BasePriceRub {
			return nil, ErrInsufficientBalance
		}
	}

	// Создаем подписку
	now := time.Now()
	sub := &models.Subscription{
		AccountID:         accountID,
		TariffVersionID:   tariffVersion.ID,
		Status:            "active",
		StartedAt:         now,
		ExpiresAt:         now.AddDate(0, 0, tariffVersion.DurationDays),
		InitialPrepaidRub: tariffVersion.PrepaidAmountRub,
	}

	if err := s.repo.CreateSubscription(ctx, sub); err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	// Создаем billing event для списания
	if req.PaymentMethod == "balance" {
		metadata := map[string]interface{}{
			"description": fmt.Sprintf("Подписка %s", tariff.Name),
		}

		// Списание с рублёвого баланса
		event := &models.BillingEvent{
			AccountID:      accountID,
			Type:           "subscription_charge",
			RealAmountRub:  -tariffVersion.BasePriceRub,
			Metadata:       metadata,
			SubscriptionID: &sub.ID,
		}
		if err := s.repo.CreateBillingEvent(ctx, event); err != nil {
			return nil, fmt.Errorf("failed to create billing event: %w", err)
		}

		// Начисление prepaid операций (если тариф включает prepaid)
		if tariffVersion.PrepaidAmountRub > 0 {
			prepaidEvent := &models.BillingEvent{
				AccountID:        accountID,
				Type:             "upgrade_bonus",
				PrepaidAmountRub: tariffVersion.PrepaidAmountRub,
				Metadata:         metadata,
				SubscriptionID:   &sub.ID,
			}
			if err := s.repo.CreateBillingEvent(ctx, prepaidEvent); err != nil {
				return nil, fmt.Errorf("failed to create prepaid billing event: %w", err)
			}
		}
	}

	return &CreateSubscriptionResponse{
		SubscriptionID: sub.ID,
		Status:         "active",
		AmountCharged:  tariffVersion.BasePriceRub,
	}, nil
}

// UpgradeSubscription апгрейдит подписку
func (s *SubscriptionService) UpgradeSubscription(ctx context.Context, accountID int64, req *UpgradeSubscriptionRequest) (*UpgradeSubscriptionResponse, error) {
	// Получаем текущую подписку
	currentSub, err := s.repo.GetActiveSubscription(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("no active subscription: %w", err)
	}

	// Получаем новый тариф
	newTariffVersion, err := s.repo.GetTariffVersionByCode(ctx, req.TariffCode)
	if err != nil {
		return nil, fmt.Errorf("tariff not found: %w", err)
	}

	// Если downgrade - откладываем до конца периода
	if newTariffVersion.BasePriceRub < currentSub.InitialPrepaidRub {
		currentSub.NextTariffVersionID = &newTariffVersion.ID
		if err := s.repo.UpdateSubscription(ctx, currentSub); err != nil {
			return nil, err
		}
		return &UpgradeSubscriptionResponse{
			SubscriptionID: currentSub.ID,
			Status:         "pending_downgrade",
			AmountCharged:  0,
		}, nil
	}

	// Upgrade - доплата разницы
	diff := newTariffVersion.BasePriceRub - currentSub.InitialPrepaidRub

	// Проверяем баланс
	balance, err := s.GetBalance(ctx, accountID)
	if err != nil {
		return nil, err
	}

	if req.PaymentMethod == "balance" && balance.RealBalanceRub < diff {
		return nil, ErrInsufficientBalance
	}

	// Обновляем подписку
	currentSub.TariffVersionID = newTariffVersion.ID
	currentSub.InitialPrepaidRub = newTariffVersion.PrepaidAmountRub
	if err := s.repo.UpdateSubscription(ctx, currentSub); err != nil {
		return nil, err
	}

	// Создаем billing event
	if req.PaymentMethod == "balance" {
		event := &models.BillingEvent{
			AccountID:      accountID,
			Type:           "subscription_upgrade",
			RealAmountRub:  -diff,
			SubscriptionID: &currentSub.ID,
		}
		if err := s.repo.CreateBillingEvent(ctx, event); err != nil {
			return nil, err
		}
	}

	return &UpgradeSubscriptionResponse{
		SubscriptionID: currentSub.ID,
		Status:         "active",
		AmountCharged:  diff,
	}, nil
}

// GetBalance получает актуальный баланс счета
func (s *SubscriptionService) GetBalance(ctx context.Context, accountID int64) (*Balance, error) {
	// Получаем снапшот
	snapshot, err := s.repo.GetAccountBalance(ctx, accountID)
	if err != nil {
		log.Printf("[GetBalance] Error getting snapshot for account %d: %v", accountID, err)
		return nil, err
	}
	log.Printf("[GetBalance] Got snapshot for account %d: real=%.2f, prepaid=%.2f", accountID, snapshot.RealBalanceRub, snapshot.PrepaidBalanceRub)

	// Получаем события после снапшота
	events, err := s.repo.GetBillingEventsSince(ctx, accountID, snapshot.UpdatedAt)
	if err != nil {
		log.Printf("[GetBalance] Error getting events: %v", err)
		return nil, err
	}
	log.Printf("[GetBalance] Got %d events", len(events))

	// Пересчитываем
	realBalance := snapshot.RealBalanceRub
	prepaidBalance := snapshot.PrepaidBalanceRub
	for _, event := range events {
		realBalance += event.RealAmountRub
		prepaidBalance += event.PrepaidAmountRub
	}

	// Получаем активную подписку (необязательно)
	sub, err := s.repo.GetActiveSubscription(ctx, accountID)
	if err != nil {
		// Отсутствие подписки - не ошибка
		log.Printf("[GetBalance] No active subscription for account %d", accountID)
		sub = nil
	}

	balance := &Balance{
		AccountID:         accountID,
		RealBalanceRub:    realBalance,
		PrepaidBalanceRub: prepaidBalance,
		CalculatedAt:      time.Now(),
	}

	if sub != nil {
		tariffVersion, err := s.repo.GetTariffVersion(ctx, sub.TariffVersionID)
		if err == nil {
			tariff, _ := s.getTariffByID(ctx, tariffVersion.TariffID)
			if tariff != nil {
				balance.ActiveTariff = &Tariff{
					Code:          tariff.Code,
					Name:          tariff.Name,
					BasePriceRub:  tariffVersion.BasePriceRub,
					PrepaidAmount: sub.InitialPrepaidRub,
				}
			}
		}
	}

	return balance, nil
}

// GetActiveSubscription получает активную подписку счета
func (s *SubscriptionService) GetActiveSubscription(ctx context.Context, accountID int64) (*GetActiveSubscriptionResponse, error) {
	sub, err := s.repo.GetActiveSubscription(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, fmt.Errorf("no active subscription")
	}

	tariffVersion, err := s.repo.GetTariffVersion(ctx, sub.TariffVersionID)
	if err != nil {
		return nil, err
	}

	tariff, err := s.getTariffByID(ctx, tariffVersion.TariffID)
	if err != nil {
		return nil, err
	}

	return &GetActiveSubscriptionResponse{
		SubscriptionID:    sub.ID,
		AccountID:         sub.AccountID,
		TariffCode:        tariff.Code,
		TariffName:        tariff.Name,
		Status:            sub.Status,
		StartedAt:         sub.StartedAt.Format("2006-01-02T15:04:05Z"),
		ExpiresAt:         sub.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		AutoRenew:         sub.AutoRenew,
		InitialPrepaidRub: sub.InitialPrepaidRub,
	}, nil
}

// CreateTopupEvent создает событие пополнения баланса
func (s *SubscriptionService) CreateTopupEvent(ctx context.Context, accountID int64, amount float64) error {
	event := &models.BillingEvent{
		AccountID:     accountID,
		Type:          "balance_topup",
		RealAmountRub: amount,
		Metadata: map[string]interface{}{
			"description": "Пополнение баланса через ЮКассу",
		},
	}
	return s.repo.CreateBillingEvent(ctx, event)
}

// SnapshotBalance создает снапшот баланса
func (s *SubscriptionService) SnapshotBalance(ctx context.Context, accountID int64) (*models.BalanceSnapshot, error) {
	balance, err := s.GetBalance(ctx, accountID)
	if err != nil {
		return nil, err
	}

	snapshot := &models.BalanceSnapshot{
		AccountID:         accountID,
		RealBalanceRub:    balance.RealBalanceRub,
		PrepaidBalanceRub: balance.PrepaidBalanceRub,
	}

	if err := s.repo.UpdateBalanceSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}

	return snapshot, nil
}

// RecalculateBalance пересчитывает баланс с нуля
func (s *SubscriptionService) RecalculateBalance(ctx context.Context, accountID int64) (*models.BalanceSnapshot, error) {
	// Получаем все события
	events, err := s.repo.GetBillingEventsSince(ctx, accountID, time.Time{})
	if err != nil {
		return nil, err
	}

	snapshot := &models.BalanceSnapshot{
		AccountID:         accountID,
		RealBalanceRub:    0,
		PrepaidBalanceRub: 0,
	}

	for _, event := range events {
		snapshot.RealBalanceRub += event.RealAmountRub
		snapshot.PrepaidBalanceRub += event.PrepaidAmountRub
	}

	return snapshot, nil
}

// GetBillingEvents получает историю биллинг-событий счёта
func (s *SubscriptionService) GetBillingEvents(ctx context.Context, accountID int64) ([]*models.BillingEvent, error) {
	return s.repo.GetBillingEventsSince(ctx, accountID, time.Time{})
}

// getTariffByID получает тариф по ID (вспомогательный метод)
func (s *SubscriptionService) getTariffByID(ctx context.Context, id int16) (*models.Tariff, error) {
	return s.repo.GetTariff(ctx, id)
}

// calculateInitialPrepaidRub вычисляет остаток prepaid
func (s *SubscriptionService) calculateInitialPrepaidRub(ctx context.Context, accountID int64, subID int32, initial float64) float64 {
	// Получаем все события списания по подписке
	// Упрощенная версия - в реальности нужно фильтровать по subscription_id
	return initial // TODO: вычесть использованное
}

// ProcessExpiredSubscriptions обрабатывает истекшие подписки (cron job)
func (s *SubscriptionService) ProcessExpiredSubscriptions(ctx context.Context) error {
	// Этот метод должен вызываться cron-джобой
	// Получаем все подписки с expires_at <= NOW() и статусом active
	// Переводим в grace_period, затем в expired
	// Обрабатываем downgrade для pending_downgrade
	return nil
}
