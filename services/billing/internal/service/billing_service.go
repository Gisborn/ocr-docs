package service

import (
	"context"
	"fmt"
	"time"

	"scan.passport.local/api/services/billing/internal/repository"
	"scan.passport.local/api/services/billing/pkg/models"
)

// BillingService сервис биллинга
type BillingService struct {
	repo repository.Repository
}

// NewBillingService создает новый сервис
func NewBillingService(repo repository.Repository) *BillingService {
	return &BillingService{repo: repo}
}

// ReserveRequest запрос на резервирование
type ReserveRequest struct {
	ServiceID string `json:"service_id"`
	RequestID string `json:"request_id"`
}

// ReserveResponse ответ на резервирование
type ReserveResponse struct {
	Reserved      bool    `json:"reserved"`
	TransactionID string  `json:"transaction_id"`
	ChargeType    string  `json:"charge_type"`
	AmountRub     float64 `json:"amount_rub"`
}

// Reserve резервирует средства для операции
func (s *BillingService) Reserve(ctx context.Context, accountID int64, req *ReserveRequest) (*ReserveResponse, error) {
	// Проверяем статус аккаунта
	account, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}
	if account.Status != "active" {
		if account.Status == "blocked" {
			return nil, ErrAccountBlocked
		}
		return nil, ErrAccountArchived
	}

	// Проверяем существующий резерв с тем же request_id (идемпотентность)
	existing, _ := s.repo.GetReservation(ctx, req.RequestID)
	if existing != nil {
		return &ReserveResponse{
			Reserved:      true,
			TransactionID: req.RequestID,
			ChargeType:    existing.ChargeType,
			AmountRub:     existing.AmountRub,
		}, nil
	}

	// Получаем баланс
	balance, err := s.repo.GetAccountBalance(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// Получаем события после снапшота для пересчета
	events, err := s.repo.GetBillingEventsSince(ctx, accountID, balance.UpdatedAt.Add(-time.Microsecond))
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	// Пересчитываем баланс
	realBalance := balance.RealBalanceRub
	prepaidBalance := balance.PrepaidBalanceRub
	for _, event := range events {
		realBalance += event.RealAmountRub
		prepaidBalance += event.PrepaidAmountRub
	}

	// Получаем активные резервы
	reservations, err := s.repo.GetActiveReservations(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get reservations: %w", err)
	}

	realReserved := 0.0
	prepaidReserved := 0.0
	for _, r := range reservations {
		if r.ChargeType == "pay_as_you_go" {
			realReserved += r.AmountRub
		} else {
			prepaidReserved += r.AmountRub
		}
	}

	availableReal := realBalance - realReserved
	availablePrepaid := prepaidBalance - prepaidReserved

	// Получаем активную подписку
	sub, err := s.repo.GetActiveSubscription(ctx, accountID)
	if err != nil {
		return nil, err
	}

	// Определяем тип списания и цену
	var chargeType string
	var amount float64

	if sub != nil && sub.Status == "active" {
		// Есть активная подписка - пробуем списать с prepaid
		price, err := s.repo.GetServicePrice(ctx, sub.TariffVersionID, req.ServiceID)
		if err == nil && price != nil {
			if availablePrepaid >= price.IncludedPriceRub {
				chargeType = "prepaid"
				amount = price.IncludedPriceRub
			} else if availableReal >= price.OveragePriceRub {
				// Prepaid закончился, списываем с баланса по overage цене
				chargeType = "pay_as_you_go"
				amount = price.OveragePriceRub
			} else {
				return nil, ErrInsufficientBalance
			}
		} else {
			// Нет цены в тарифе, используем pay-as-you-go
			if availableReal > 0 {
				chargeType = "pay_as_you_go"
				amount = 7.00 // дефолтная цена
			} else {
				return nil, ErrInsufficientBalance
			}
		}
	} else {
		// Нет активной подписки - только pay-as-you-go
		if availableReal > 0 {
			chargeType = "pay_as_you_go"
			amount = 7.00 // дефолтная цена
		} else {
			return nil, ErrInsufficientBalance
		}
	}

	// Создаем резерв
	reservation := &models.Reservation{
		AccountID:  accountID,
		ServiceID:  &req.ServiceID,
		RequestID:  req.RequestID,
		AmountRub:  amount,
		ChargeType: chargeType,
		ExpiresAt:  time.Now().Add(5 * time.Minute),
	}

	if err := s.repo.CreateReservation(ctx, reservation); err != nil {
		return nil, fmt.Errorf("failed to create reservation: %w", err)
	}

	return &ReserveResponse{
		Reserved:      true,
		TransactionID: req.RequestID,
		ChargeType:    chargeType,
		AmountRub:     amount,
	}, nil
}

// Commit фиксирует списание
func (s *BillingService) Commit(ctx context.Context, requestID string) error {
	// Получаем резерв
	res, err := s.repo.GetReservation(ctx, requestID)
	if err != nil {
		return fmt.Errorf("reservation not found: %w", err)
	}

	// Удаляем резерв
	if err := s.repo.DeleteReservation(ctx, requestID); err != nil {
		return err
	}

	// Создаем событие списания
	event := &models.BillingEvent{
		AccountID: res.AccountID,
		ServiceID: res.ServiceID,
		Type:      s.chargeTypeToEventType(res.ChargeType),
		RequestID: &requestID,
	}

	if res.ChargeType == "pay_as_you_go" {
		event.RealAmountRub = -res.AmountRub
	} else {
		event.PrepaidAmountRub = -res.AmountRub
	}

	return s.repo.CreateBillingEvent(ctx, event)
}

// Rollback откатывает резерв
func (s *BillingService) Rollback(ctx context.Context, requestID string, reason string) error {
	// Просто удаляем резерв, деньги не списывались
	return s.repo.DeleteReservation(ctx, requestID)
}

// chargeTypeToEventType преобразует тип списания в тип события
func (s *BillingService) chargeTypeToEventType(chargeType string) string {
	if chargeType == "prepaid" {
		return "prepaid_usage"
	}
	return "pay_as_you_go"
}

// CreateAccount создает новый счет
func (s *BillingService) CreateAccount(ctx context.Context) (*models.Account, error) {
	return s.repo.CreateAccount(ctx)
}

// Errors
var (
	ErrAccountBlocked      = fmt.Errorf("account is blocked")
	ErrAccountArchived     = fmt.Errorf("account is archived")
	ErrInsufficientBalance = fmt.Errorf("insufficient balance")
)
