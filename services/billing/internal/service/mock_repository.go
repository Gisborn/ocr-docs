package service

import (
	"context"
	"fmt"
	"time"

	billingrepo "scan.passport.local/api/services/billing/internal/repository"
	"scan.passport.local/api/services/billing/pkg/models"
	"github.com/jackc/pgx/v5"
)

// MockRepository мок репозитория для тестирования
type MockRepository struct {
	accounts       map[int64]*models.Account
	balances       map[int64]*models.BalanceSnapshot
	reservations   map[string]*models.Reservation
	events         []*models.BillingEvent
	subscriptions  map[int32]*models.Subscription
	tariffs        map[int16]*models.Tariff
	tariffVersions map[int32]*models.TariffVersion
	prices         map[string]*models.TariffServicePrice
	orders         map[int32]*models.PaymentOrder
	nextAccountID  int64
	nextSubID      int32
}

// NewMockRepository создает новый мок репозитория
func NewMockRepository() *MockRepository {
	return &MockRepository{
		accounts:       make(map[int64]*models.Account),
		balances:       make(map[int64]*models.BalanceSnapshot),
		reservations:   make(map[string]*models.Reservation),
		events:         make([]*models.BillingEvent, 0),
		subscriptions:  make(map[int32]*models.Subscription),
		tariffs:        make(map[int16]*models.Tariff),
		tariffVersions: make(map[int32]*models.TariffVersion),
		prices:         make(map[string]*models.TariffServicePrice),
		orders:         make(map[int32]*models.PaymentOrder),
		nextAccountID:  1,
		nextSubID:      1,
	}
}

func (m *MockRepository) CreateAccount(ctx context.Context) (*models.Account, error) {
	acc := &models.Account{
		ID:        m.nextAccountID,
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.accounts[acc.ID] = acc
	m.balances[acc.ID] = &models.BalanceSnapshot{
		AccountID:         acc.ID,
		RealBalanceRub:    0,
		PrepaidBalanceRub: 0,
		UpdatedAt:         time.Now().Add(-time.Microsecond),
	}
	m.nextAccountID++
	return acc, nil
}

func (m *MockRepository) GetAccount(ctx context.Context, id int64) (*models.Account, error) {
	acc, ok := m.accounts[id]
	if !ok {
		return nil, fmt.Errorf("account not found")
	}
	return acc, nil
}

func (m *MockRepository) GetAccountBalance(ctx context.Context, accountID int64) (*models.BalanceSnapshot, error) {
	bal, ok := m.balances[accountID]
	if !ok {
		return nil, fmt.Errorf("balance not found")
	}
	return bal, nil
}

func (m *MockRepository) UpdateBalanceSnapshot(ctx context.Context, snapshot *models.BalanceSnapshot) error {
	snapshot.UpdatedAt = time.Now().Add(-time.Microsecond)
	m.balances[snapshot.AccountID] = snapshot
	return nil
}

func (m *MockRepository) CreateReservation(ctx context.Context, r *models.Reservation) error {
	r.ID = int64(len(m.reservations) + 1)
	r.CreatedAt = time.Now()
	m.reservations[r.RequestID] = r
	return nil
}

func (m *MockRepository) GetReservation(ctx context.Context, requestID string) (*models.Reservation, error) {
	r, ok := m.reservations[requestID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return r, nil
}

func (m *MockRepository) DeleteReservation(ctx context.Context, requestID string) error {
	delete(m.reservations, requestID)
	return nil
}

func (m *MockRepository) GetActiveReservations(ctx context.Context, accountID int64) ([]*models.Reservation, error) {
	var result []*models.Reservation
	for _, r := range m.reservations {
		if r.AccountID == accountID && r.ExpiresAt.After(time.Now()) {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *MockRepository) DeleteExpiredReservations(ctx context.Context) error {
	return nil
}

func (m *MockRepository) CreateBillingEvent(ctx context.Context, e *models.BillingEvent) error {
	e.ID = int64(len(m.events) + 1)
	e.CreatedAt = time.Now()
	m.events = append(m.events, e)
	return nil
}

func (m *MockRepository) GetBillingEventsSince(ctx context.Context, accountID int64, since time.Time) ([]*models.BillingEvent, error) {
	var result []*models.BillingEvent
	for _, e := range m.events {
		if e.AccountID == accountID && e.CreatedAt.After(since) {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *MockRepository) CreateSubscription(ctx context.Context, s *models.Subscription) error {
	s.ID = m.nextSubID
	s.CreatedAt = time.Now()
	s.UpdatedAt = time.Now()
	m.subscriptions[s.ID] = s
	m.nextSubID++
	return nil
}

func (m *MockRepository) GetActiveSubscription(ctx context.Context, accountID int64) (*models.Subscription, error) {
	for _, s := range m.subscriptions {
		if s.AccountID == accountID && (s.Status == "active" || s.Status == "grace_period") {
			return s, nil
		}
	}
	return nil, nil
}

func (m *MockRepository) UpdateSubscription(ctx context.Context, s *models.Subscription) error {
	s.UpdatedAt = time.Now()
	m.subscriptions[s.ID] = s
	return nil
}

func (m *MockRepository) GetSubscription(ctx context.Context, id int32) (*models.Subscription, error) {
	return m.subscriptions[id], nil
}

func (m *MockRepository) GetTariffVersion(ctx context.Context, id int32) (*models.TariffVersion, error) {
	return m.tariffVersions[id], nil
}

func (m *MockRepository) GetTariff(ctx context.Context, id int16) (*models.Tariff, error) {
	return m.tariffs[id], nil
}

func (m *MockRepository) GetTariffVersionByCode(ctx context.Context, code string) (*models.TariffVersion, error) {
	for _, tv := range m.tariffVersions {
		if t, ok := m.tariffs[tv.TariffID]; ok && t.Code == code {
			return tv, nil
		}
	}
	return nil, fmt.Errorf("tariff not found")
}

func (m *MockRepository) GetTariffs(ctx context.Context) ([]*models.TariffWithVersion, error) {
	var result []*models.TariffWithVersion
	for _, tv := range m.tariffVersions {
		if t, ok := m.tariffs[tv.TariffID]; ok {
			result = append(result, &models.TariffWithVersion{
				ID:               t.ID,
				Code:             t.Code,
				Name:             t.Name,
				Description:      t.Description,
				BasePriceRub:     tv.BasePriceRub,
				PrepaidAmountRub: tv.PrepaidAmountRub,
				DurationDays:     tv.DurationDays,
			})
		}
	}
	return result, nil
}

func (m *MockRepository) GetServicePrice(ctx context.Context, tariffVersionID int32, serviceID string) (*models.TariffServicePrice, error) {
	key := fmt.Sprintf("%d_%s", tariffVersionID, serviceID)
	return m.prices[key], nil
}

func (m *MockRepository) CreatePaymentOrder(ctx context.Context, o *models.PaymentOrder) error {
	o.ID = int32(len(m.orders) + 1)
	if o.CreatedAt.IsZero() {
		o.CreatedAt = time.Now()
	}
	m.orders[o.ID] = o
	return nil
}

func (m *MockRepository) GetPaymentOrder(ctx context.Context, id int32) (*models.PaymentOrder, error) {
	order, ok := m.orders[id]
	if !ok {
		return nil, fmt.Errorf("payment order not found")
	}
	return order, nil
}

func (m *MockRepository) GetPaymentOrderByYookassaID(ctx context.Context, paymentID string) (*models.PaymentOrder, error) {
	for _, o := range m.orders {
		if o.YookassaPaymentID != nil && *o.YookassaPaymentID == paymentID {
			return o, nil
		}
	}
	return nil, nil
}

func (m *MockRepository) UpdatePaymentOrder(ctx context.Context, o *models.PaymentOrder) error {
	m.orders[o.ID] = o
	return nil
}

func (m *MockRepository) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return nil, nil
}

func (m *MockRepository) WithTx(tx pgx.Tx) billingrepo.Repository {
	return m
}

func (m *MockRepository) GetExpiredPendingPayments(ctx context.Context, timeout time.Duration) ([]*models.PaymentOrder, error) {
	var result []*models.PaymentOrder
	cutoff := time.Now().Add(-timeout)
	for _, o := range m.orders {
		if o.Status == "pending" && o.CreatedAt.Before(cutoff) {
			result = append(result, o)
		}
	}
	return result, nil
}

// SetBalance устанавливает баланс аккаунта
func (m *MockRepository) SetBalance(accountID int64, realRub, prepaidRub float64) {
	if b, ok := m.balances[accountID]; ok {
		b.RealBalanceRub = realRub
		b.PrepaidBalanceRub = prepaidRub
	}
}

// SetAccountStatus меняет статус аккаунта
func (m *MockRepository) SetAccountStatus(accountID int64, status string) {
	if acc, ok := m.accounts[accountID]; ok {
		acc.Status = status
	}
}

// SeedTestData заполняет мок начальными данными для тестов
func (m *MockRepository) SeedTestData() {
	// Тариф "start"
	m.tariffs[1] = &models.Tariff{ID: 1, Code: "start", Name: "Старт"}
	m.tariffVersions[1] = &models.TariffVersion{
		ID:              1,
		TariffID:        1,
		BasePriceRub:    0,
		PrepaidAmountRub: 0,
		DurationDays:    30,
	}

	// Тариф "pro"
	m.tariffs[2] = &models.Tariff{ID: 2, Code: "pro", Name: "Профессиональный"}
	m.tariffVersions[2] = &models.TariffVersion{
		ID:              2,
		TariffID:        2,
		BasePriceRub:    10000,
		PrepaidAmountRub: 10000,
		DurationDays:    30,
	}

	// Цена на распознавание паспорта для pro (included slightly less than overage)
	m.prices["2_passport_rf"] = &models.TariffServicePrice{
		TariffVersionID:   2,
		ServiceID:         "passport_rf",
		IncludedPriceRub:  2.50,
		OveragePriceRub:   3.00,
	}
}
