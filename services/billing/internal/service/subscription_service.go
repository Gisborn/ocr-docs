package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/api-scan/api-scan/services/billing/internal/repository"
	"github.com/api-scan/api-scan/services/billing/pkg/models"
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
	ID                int32   `json:"id"`
	TariffCode        string  `json:"tariff_code"`
	Status            string  `json:"status"`
	StartedAt         time.Time `json:"started_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	PrepaidRemaining  float64 `json:"prepaid_remaining_rub"`
}

// CreateSubscription создает новую подписку
func (s *SubscriptionService) CreateSubscription(ctx context.Context, accountID int64, req *CreateSubscriptionRequest) (*CreateSubscriptionResponse, error) {
	// Получаем версию тарифа
	tariffVersion, err := s.repo.GetTariffVersionByCode(ctx, req.TariffCode)
	if err != nil {
		return nil, fmt.Errorf("tariff not found: %w", err)
	}

	// Проверяем что аккаунт активен
	account, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Status != "active" {
		return nil, fmt.Errorf("account is not active")
	}

	// Проверяем/отменяем текущую активную подписку
	currentSub, err := s.repo.GetActiveSubscription(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if currentSub != nil {
		// Отменяем текущую подписку
		currentSub.Status = "cancelled"
		if err := s.repo.UpdateSubscription(ctx, currentSub); err != nil {
			return nil, err
		}
	}

	// Если оплата с баланса - проверяем и списываем
	if req.PaymentMethod == "balance" {
		balance, err := s.getActualBalance(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if balance.RealBalanceRub < tariffVersion.BasePriceRub {
			return nil, ErrInsufficientBalance
		}

		// Создаем событие оплаты подписки
		event := &models.BillingEvent{
			AccountID:      accountID,
			Type:           "subscription_payment",
			RealAmountRub:  -tariffVersion.BasePriceRub,
		}
		if err := s.repo.CreateBillingEvent(ctx, event); err != nil {
			return nil, err
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
		AutoRenew:         false,
	}

	if err := s.repo.CreateSubscription(ctx, sub); err != nil {
		return nil, err
	}

	// Создаем событие начисления prepaid
	if tariffVersion.PrepaidAmountRub > 0 {
		event := &models.BillingEvent{
			AccountID:        accountID,
			SubscriptionID:   &sub.ID,
			Type:             "subscription_payment",
			PrepaidAmountRub: tariffVersion.PrepaidAmountRub,
		}
		if err := s.repo.CreateBillingEvent(ctx, event); err != nil {
			return nil, err
		}
	}

	return &CreateSubscriptionResponse{
		ID:               sub.ID,
		TariffCode:       req.TariffCode,
		Status:           sub.Status,
		StartedAt:        sub.StartedAt,
		ExpiresAt:        sub.ExpiresAt,
		PrepaidRemaining: tariffVersion.PrepaidAmountRub,
	}, nil
}

// UpgradeRequest запрос на апгрейд
type UpgradeRequest struct {
	TariffCode    string `json:"tariff_code"`
	PaymentMethod string `json:"payment_method"`
}

// UpgradeResponse ответ на апгрейд
type UpgradeResponse struct {
	SubscriptionID    int32   `json:"subscription_id"`
	PreviousTariff    string  `json:"previous_tariff"`
	NewTariff         string  `json:"new_tariff"`
	ProratedBonusRub  float64 `json:"prorated_bonus_rub"`
	TotalChargeRub    float64 `json:"total_charge_rub"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// Upgrade выполняет апгрейд подписки
func (s *SubscriptionService) Upgrade(ctx context.Context, accountID int64, req *UpgradeRequest) (*UpgradeResponse, error) {
	// Получаем текущую подписку
	currentSub, err := s.repo.GetActiveSubscription(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if currentSub == nil {
		return nil, fmt.Errorf("no active subscription")
	}

	// Получаем текущий тариф
	currentVersion, err := s.repo.GetTariffVersion(ctx, currentSub.TariffVersionID)
	if err != nil {
		return nil, err
	}
	currentTariff, err := s.getTariffByID(ctx, currentVersion.TariffID)
	if err != nil {
		return nil, err
	}

	// Получаем новый тариф
	newVersion, err := s.repo.GetTariffVersionByCode(ctx, req.TariffCode)
	if err != nil {
		return nil, fmt.Errorf("new tariff not found: %w", err)
	}
	newTariff, err := s.getTariffByID(ctx, newVersion.TariffID)
	if err != nil {
		return nil, err
	}

	// Вычисляем оставшиеся дни
	daysRemaining := int(currentSub.ExpiresAt.Sub(time.Now()).Hours() / 24)
	totalDays := currentVersion.DurationDays
	if daysRemaining < 0 {
		daysRemaining = 0
	}
	if daysRemaining > totalDays {
		daysRemaining = totalDays
	}

	// Вычисляем стоимость по формуле
	// total_charge = (new_monthly - old_monthly) * (days_remaining / 30)
	//              - (new_prepaid - old_prepaid) * (days_remaining / 30)
	
	monthRatio := float64(daysRemaining) / float64(totalDays)
	
	upgradeCost := (newVersion.BasePriceRub - currentVersion.BasePriceRub) * monthRatio
	prepaidDiff := (newVersion.PrepaidAmountRub - currentVersion.PrepaidAmountRub) * monthRatio
	
	totalCharge := upgradeCost - prepaidDiff

	// Если нужно доплатить
	if totalCharge > 0 && req.PaymentMethod == "balance" {
		balance, err := s.getActualBalance(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if balance.RealBalanceRub < totalCharge {
			return nil, ErrInsufficientBalance
		}

		// Списываем с баланса
		event := &models.BillingEvent{
			AccountID:      accountID,
			SubscriptionID: &currentSub.ID,
			Type:           "upgrade_payment",
			RealAmountRub:  -totalCharge,
		}
		if err := s.repo.CreateBillingEvent(ctx, event); err != nil {
			return nil, err
		}
	} else if totalCharge < 0 {
		// Бонус на баланс (редкий случай downgrade)
		event := &models.BillingEvent{
			AccountID:      accountID,
			SubscriptionID: &currentSub.ID,
			Type:           "upgrade_bonus",
			RealAmountRub:  -totalCharge, // отрицательное число
		}
		if err := s.repo.CreateBillingEvent(ctx, event); err != nil {
			return nil, err
		}
	}

	// Обновляем подписку
	currentSub.TariffVersionID = newVersion.ID
	currentSub.InitialPrepaidRub = newVersion.PrepaidAmountRub
	if err := s.repo.UpdateSubscription(ctx, currentSub); err != nil {
		return nil, err
	}

	// Начисляем разницу в prepaid
	if prepaidDiff > 0 {
		event := &models.BillingEvent{
			AccountID:        accountID,
			SubscriptionID:   &currentSub.ID,
			Type:             "upgrade_bonus",
			PrepaidAmountRub: prepaidDiff,
		}
		if err := s.repo.CreateBillingEvent(ctx, event); err != nil {
			return nil, err
		}
	}

	return &UpgradeResponse{
		SubscriptionID:   currentSub.ID,
		PreviousTariff:   currentTariff.Code,
		NewTariff:        newTariff.Code,
		ProratedBonusRub: math.Max(0, prepaidDiff),
		TotalChargeRub:   math.Max(0, totalCharge),
		ExpiresAt:        currentSub.ExpiresAt,
	}, nil
}

// DowngradeRequest запрос на даунгрейд (отложенный)
type DowngradeRequest struct {
	TariffCode string `json:"tariff_code"`
}

// Downgrade выполняет отложенный даунгрейд
func (s *SubscriptionService) Downgrade(ctx context.Context, accountID int64, req *DowngradeRequest) error {
	// Получаем текущую подписку
	currentSub, err := s.repo.GetActiveSubscription(ctx, accountID)
	if err != nil {
		return err
	}
	if currentSub == nil {
		return fmt.Errorf("no active subscription")
	}

	// Получаем новый тариф
	newVersion, err := s.repo.GetTariffVersionByCode(ctx, req.TariffCode)
	if err != nil {
		return fmt.Errorf("tariff not found: %w", err)
	}

	// Устанавливаем next_tariff_version_id
	currentSub.NextTariffVersionID = &newVersion.ID
	return s.repo.UpdateSubscription(ctx, currentSub)
}

// GetBalance получает актуальный баланс счета
func (s *SubscriptionService) GetBalance(ctx context.Context, accountID int64) (*models.Balance, error) {
	// Получаем снапшот
	snapshot, err := s.repo.GetAccountBalance(ctx, accountID)
	if err != nil {
		return nil, err
	}

	// Получаем события после снапшота
	events, err := s.repo.GetBillingEventsSince(ctx, accountID, snapshot.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// Пересчитываем
	realBalance := snapshot.RealBalanceRub
	prepaidBalance := snapshot.PrepaidBalanceRub
	for _, event := range events {
		realBalance += event.RealAmountRub
		prepaidBalance += event.PrepaidAmountRub
	}

	// Получаем активную подписку
	sub, err := s.repo.GetActiveSubscription(ctx, accountID)
	if err != nil {
		return nil, err
	}

	balance := &models.Balance{
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
				// Вычисляем остаток prepaid
				prepaidRemaining := s.calculatePrepaidRemaining(ctx, accountID, sub.ID, tariffVersion.PrepaidAmountRub)
				
				balance.ActiveSubscription = &models.SubscriptionInfo{
					ID:               sub.ID,
					TariffCode:       tariff.Code,
					TariffName:       tariff.Name,
					ExpiresAt:        sub.ExpiresAt,
					Status:           sub.Status,
					PrepaidRemaining: prepaidRemaining,
				}
			}
		}
	}

	return balance, nil
}

// getActualBalance получает актуальный баланс
func (s *SubscriptionService) getActualBalance(ctx context.Context, accountID int64) (*models.BalanceSnapshot, error) {
	snapshot, err := s.repo.GetAccountBalance(ctx, accountID)
	if err != nil {
		return nil, err
	}

	events, err := s.repo.GetBillingEventsSince(ctx, accountID, snapshot.UpdatedAt)
	if err != nil {
		return nil, err
	}

	for _, event := range events {
		snapshot.RealBalanceRub += event.RealAmountRub
		snapshot.PrepaidBalanceRub += event.PrepaidAmountRub
	}

	return snapshot, nil
}

// getTariffByID получает тариф по ID (вспомогательный метод)
func (s *SubscriptionService) getTariffByID(ctx context.Context, id int16) (*models.Tariff, error) {
	return s.repo.GetTariff(ctx, id)
}

// calculatePrepaidRemaining вычисляет остаток prepaid
func (s *SubscriptionService) calculatePrepaidRemaining(ctx context.Context, accountID int64, subID int32, initial float64) float64 {
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
