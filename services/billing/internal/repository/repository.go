package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/api-scan/api-scan/services/billing/pkg/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository интерфейс для работы с БД
type Repository interface {
	// Accounts
	CreateAccount(ctx context.Context) (*models.Account, error)
	GetAccount(ctx context.Context, id int64) (*models.Account, error)
	GetAccountBalance(ctx context.Context, accountID int64) (*models.BalanceSnapshot, error)
	UpdateBalanceSnapshot(ctx context.Context, snapshot *models.BalanceSnapshot) error
	
	// Reservations
	CreateReservation(ctx context.Context, reservation *models.Reservation) error
	GetReservation(ctx context.Context, requestID string) (*models.Reservation, error)
	DeleteReservation(ctx context.Context, requestID string) error
	GetActiveReservations(ctx context.Context, accountID int64) ([]*models.Reservation, error)
	DeleteExpiredReservations(ctx context.Context) error
	
	// Billing Events
	CreateBillingEvent(ctx context.Context, event *models.BillingEvent) error
	GetBillingEventsSince(ctx context.Context, accountID int64, since time.Time) ([]*models.BillingEvent, error)
	
	// Subscriptions
	CreateSubscription(ctx context.Context, sub *models.Subscription) error
	GetActiveSubscription(ctx context.Context, accountID int64) (*models.Subscription, error)
	UpdateSubscription(ctx context.Context, sub *models.Subscription) error
	GetSubscription(ctx context.Context, id int32) (*models.Subscription, error)
	
	// Tariffs
	GetTariff(ctx context.Context, id int16) (*models.Tariff, error)
	GetTariffVersion(ctx context.Context, id int32) (*models.TariffVersion, error)
	GetTariffVersionByCode(ctx context.Context, code string) (*models.TariffVersion, error)
	GetServicePrice(ctx context.Context, tariffVersionID int32, serviceID string) (*models.TariffServicePrice, error)
	
	// Payment Orders
	CreatePaymentOrder(ctx context.Context, order *models.PaymentOrder) error
	GetPaymentOrder(ctx context.Context, id int32) (*models.PaymentOrder, error)
	GetPaymentOrderByYookassaID(ctx context.Context, paymentID string) (*models.PaymentOrder, error)
	UpdatePaymentOrder(ctx context.Context, order *models.PaymentOrder) error
	GetExpiredPendingPayments(ctx context.Context, timeout time.Duration) ([]*models.PaymentOrder, error)
	
	// Transaction support
	BeginTx(ctx context.Context) (pgx.Tx, error)
	WithTx(tx pgx.Tx) Repository
}

// PostgresRepository реализация на PostgreSQL
type PostgresRepository struct {
	pool *pgxpool.Pool
	tx   pgx.Tx
}

// NewPostgresRepository создает новый репозиторий
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// WithTx возвращает репозиторий с транзакцией
func (r *PostgresRepository) WithTx(tx pgx.Tx) Repository {
	return &PostgresRepository{pool: r.pool, tx: tx}
}

// BeginTx начинает транзакцию
func (r *PostgresRepository) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

// query выполняет запрос (в транзакции или нет)
func (r *PostgresRepository) query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if r.tx != nil {
		return r.tx.Query(ctx, sql, args...)
	}
	return r.pool.Query(ctx, sql, args...)
}

// queryRow выполняет запрос на одну строку
func (r *PostgresRepository) queryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if r.tx != nil {
		return r.tx.QueryRow(ctx, sql, args...)
	}
	return r.pool.QueryRow(ctx, sql, args...)
}

// exec выполняет запрос без возврата данных
func (r *PostgresRepository) exec(ctx context.Context, sql string, args ...interface{}) error {
	if r.tx != nil {
		_, err := r.tx.Exec(ctx, sql, args...)
		return err
	}
	_, err := r.pool.Exec(ctx, sql, args...)
	return err
}

// CreateAccount создает новый счет
func (r *PostgresRepository) CreateAccount(ctx context.Context) (*models.Account, error) {
	account := &models.Account{}
	err := r.queryRow(ctx, 
		`INSERT INTO accounts (status, created_at, updated_at) 
		 VALUES ('active', NOW(), NOW()) 
		 RETURNING id, status, created_at, updated_at`,
	).Scan(&account.ID, &account.Status, &account.CreatedAt, &account.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	
	// Создаем начальный снапшот баланса
	err = r.exec(ctx,
		`INSERT INTO balance_snapshots (account_id, real_balance_rub, prepaid_balance_rub, updated_at)
		 VALUES ($1, 0, 0, NOW())
		 ON CONFLICT (account_id) DO NOTHING`,
		account.ID,
	)
	
	return account, err
}

// GetAccount получает счет по ID
func (r *PostgresRepository) GetAccount(ctx context.Context, id int64) (*models.Account, error) {
	account := &models.Account{}
	err := r.queryRow(ctx,
		`SELECT id, status, created_at, updated_at FROM accounts WHERE id = $1`,
		id,
	).Scan(&account.ID, &account.Status, &account.CreatedAt, &account.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	return account, nil
}

// GetAccountBalance получает снапшот баланса
func (r *PostgresRepository) GetAccountBalance(ctx context.Context, accountID int64) (*models.BalanceSnapshot, error) {
	snapshot := &models.BalanceSnapshot{}
	err := r.queryRow(ctx,
		`SELECT account_id, real_balance_rub, prepaid_balance_rub, updated_at 
		 FROM balance_snapshots WHERE account_id = $1`,
		accountID,
	).Scan(&snapshot.AccountID, &snapshot.RealBalanceRub, &snapshot.PrepaidBalanceRub, &snapshot.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	return snapshot, nil
}

// UpdateBalanceSnapshot обновляет снапшот баланса
func (r *PostgresRepository) UpdateBalanceSnapshot(ctx context.Context, snapshot *models.BalanceSnapshot) error {
	return r.exec(ctx,
		`UPDATE balance_snapshots 
		 SET real_balance_rub = $1, prepaid_balance_rub = $2, updated_at = NOW()
		 WHERE account_id = $3`,
		snapshot.RealBalanceRub, snapshot.PrepaidBalanceRub, snapshot.AccountID,
	)
}

// CreateReservation создает резерв
func (r *PostgresRepository) CreateReservation(ctx context.Context, reservation *models.Reservation) error {
	return r.queryRow(ctx,
		`INSERT INTO reservations (account_id, subscription_id, service_id, request_id, amount_rub, charge_type, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7)
		 RETURNING id, created_at`,
		reservation.AccountID, reservation.SubscriptionID, reservation.ServiceID,
		reservation.RequestID, reservation.AmountRub, reservation.ChargeType, reservation.ExpiresAt,
	).Scan(&reservation.ID, &reservation.CreatedAt)
}

// GetReservation получает резерв по request_id
func (r *PostgresRepository) GetReservation(ctx context.Context, requestID string) (*models.Reservation, error) {
	res := &models.Reservation{}
	err := r.queryRow(ctx,
		`SELECT id, account_id, subscription_id, service_id, request_id, amount_rub, charge_type, created_at, expires_at
		 FROM reservations WHERE request_id = $1`,
		requestID,
	).Scan(&res.ID, &res.AccountID, &res.SubscriptionID, &res.ServiceID, &res.RequestID,
		&res.AmountRub, &res.ChargeType, &res.CreatedAt, &res.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// DeleteReservation удаляет резерв
func (r *PostgresRepository) DeleteReservation(ctx context.Context, requestID string) error {
	return r.exec(ctx, `DELETE FROM reservations WHERE request_id = $1`, requestID)
}

// GetActiveReservations получает активные резервы счета
func (r *PostgresRepository) GetActiveReservations(ctx context.Context, accountID int64) ([]*models.Reservation, error) {
	rows, err := r.query(ctx,
		`SELECT id, account_id, subscription_id, service_id, request_id, amount_rub, charge_type, created_at, expires_at
		 FROM reservations WHERE account_id = $1 AND expires_at > NOW()`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var reservations []*models.Reservation
	for rows.Next() {
		res := &models.Reservation{}
		if err := rows.Scan(&res.ID, &res.AccountID, &res.SubscriptionID, &res.ServiceID, &res.RequestID,
			&res.AmountRub, &res.ChargeType, &res.CreatedAt, &res.ExpiresAt); err != nil {
			return nil, err
		}
		reservations = append(reservations, res)
	}
	return reservations, rows.Err()
}

// DeleteExpiredReservations удаляет просроченные резервы
func (r *PostgresRepository) DeleteExpiredReservations(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM reservations WHERE expires_at <= NOW()`)
	return err
}

// CreateBillingEvent создает событие биллинга
func (r *PostgresRepository) CreateBillingEvent(ctx context.Context, event *models.BillingEvent) error {
	return r.queryRow(ctx,
		`INSERT INTO billing_events (account_id, subscription_id, service_id, type, real_amount_rub, prepaid_amount_rub, request_id, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		 RETURNING id, created_at`,
		event.AccountID, event.SubscriptionID, event.ServiceID, event.Type,
		event.RealAmountRub, event.PrepaidAmountRub, event.RequestID, event.Metadata,
	).Scan(&event.ID, &event.CreatedAt)
}

// GetBillingEventsSince получает события с указанного времени
func (r *PostgresRepository) GetBillingEventsSince(ctx context.Context, accountID int64, since time.Time) ([]*models.BillingEvent, error) {
	rows, err := r.query(ctx,
		`SELECT id, account_id, subscription_id, service_id, type, real_amount_rub, prepaid_amount_rub, request_id, metadata, created_at
		 FROM billing_events WHERE account_id = $1 AND created_at > $2`,
		accountID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var events []*models.BillingEvent
	for rows.Next() {
		event := &models.BillingEvent{}
		if err := rows.Scan(&event.ID, &event.AccountID, &event.SubscriptionID, &event.ServiceID,
			&event.Type, &event.RealAmountRub, &event.PrepaidAmountRub, &event.RequestID,
			&event.Metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// CreateSubscription создает подписку
func (r *PostgresRepository) CreateSubscription(ctx context.Context, sub *models.Subscription) error {
	return r.queryRow(ctx,
		`INSERT INTO subscriptions (account_id, tariff_version_id, status, started_at, expires_at, initial_prepaid_rub, auto_renew, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		 RETURNING id, created_at, updated_at`,
		sub.AccountID, sub.TariffVersionID, sub.Status, sub.StartedAt, sub.ExpiresAt,
		sub.InitialPrepaidRub, sub.AutoRenew,
	).Scan(&sub.ID, &sub.CreatedAt, &sub.UpdatedAt)
}

// GetActiveSubscription получает активную подписку счета
func (r *PostgresRepository) GetActiveSubscription(ctx context.Context, accountID int64) (*models.Subscription, error) {
	sub := &models.Subscription{}
	err := r.queryRow(ctx,
		`SELECT id, account_id, tariff_version_id, status, started_at, expires_at, grace_period_ends_at,
		        initial_prepaid_rub, auto_renew, next_tariff_version_id, created_at, updated_at
		 FROM subscriptions 
		 WHERE account_id = $1 AND status IN ('active', 'grace_period')
		 ORDER BY 
		   CASE status WHEN 'active' THEN 1 WHEN 'grace_period' THEN 2 END,
		   expires_at DESC
		 LIMIT 1`,
		accountID,
	).Scan(&sub.ID, &sub.AccountID, &sub.TariffVersionID, &sub.Status, &sub.StartedAt, &sub.ExpiresAt,
		&sub.GracePeriodEndsAt, &sub.InitialPrepaidRub, &sub.AutoRenew, &sub.NextTariffVersionID,
		&sub.CreatedAt, &sub.UpdatedAt)
	
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// UpdateSubscription обновляет подписку
func (r *PostgresRepository) UpdateSubscription(ctx context.Context, sub *models.Subscription) error {
	return r.exec(ctx,
		`UPDATE subscriptions 
		 SET status = $1, expires_at = $2, grace_period_ends_at = $3, next_tariff_version_id = $4, updated_at = NOW()
		 WHERE id = $5`,
		sub.Status, sub.ExpiresAt, sub.GracePeriodEndsAt, sub.NextTariffVersionID, sub.ID,
	)
}

// GetSubscription получает подписку по ID
func (r *PostgresRepository) GetSubscription(ctx context.Context, id int32) (*models.Subscription, error) {
	sub := &models.Subscription{}
	err := r.queryRow(ctx,
		`SELECT id, account_id, tariff_version_id, status, started_at, expires_at, grace_period_ends_at,
		        initial_prepaid_rub, auto_renew, next_tariff_version_id, created_at, updated_at
		 FROM subscriptions WHERE id = $1`, id,
	).Scan(&sub.ID, &sub.AccountID, &sub.TariffVersionID, &sub.Status, &sub.StartedAt, &sub.ExpiresAt,
		&sub.GracePeriodEndsAt, &sub.InitialPrepaidRub, &sub.AutoRenew, &sub.NextTariffVersionID,
		&sub.CreatedAt, &sub.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// GetTariffVersion получает версию тарифа
func (r *PostgresRepository) GetTariffVersion(ctx context.Context, id int32) (*models.TariffVersion, error) {
	tv := &models.TariffVersion{}
	err := r.queryRow(ctx,
		`SELECT id, tariff_id, valid_from, valid_until, duration_days, base_price_rub, prepaid_amount_rub, created_at
		 FROM tariff_versions WHERE id = $1`, id,
	).Scan(&tv.ID, &tv.TariffID, &tv.ValidFrom, &tv.ValidUntil, &tv.DurationDays,
		&tv.BasePriceRub, &tv.PrepaidAmountRub, &tv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return tv, nil
}

// GetTariffVersionByCode получает актуальную версию тарифа по коду
func (r *PostgresRepository) GetTariffVersionByCode(ctx context.Context, code string) (*models.TariffVersion, error) {
	tv := &models.TariffVersion{}
	err := r.queryRow(ctx,
		`SELECT tv.id, tv.tariff_id, tv.valid_from, tv.valid_until, tv.duration_days, tv.base_price_rub, tv.prepaid_amount_rub, tv.created_at
		 FROM tariff_versions tv
		 JOIN tariffs t ON t.id = tv.tariff_id
		 WHERE t.code = $1 AND tv.valid_from <= NOW() 
		   AND (tv.valid_until IS NULL OR tv.valid_until > NOW())
		 ORDER BY tv.valid_from DESC
		 LIMIT 1`, code,
	).Scan(&tv.ID, &tv.TariffID, &tv.ValidFrom, &tv.ValidUntil, &tv.DurationDays,
		&tv.BasePriceRub, &tv.PrepaidAmountRub, &tv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return tv, nil
}

// GetTariff получает тариф по ID
func (r *PostgresRepository) GetTariff(ctx context.Context, id int16) (*models.Tariff, error) {
	tariff := &models.Tariff{}
	err := r.queryRow(ctx,
		`SELECT id, code, name, description, created_at, updated_at FROM tariffs WHERE id = $1`, id,
	).Scan(&tariff.ID, &tariff.Code, &tariff.Name, &tariff.Description, &tariff.CreatedAt, &tariff.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return tariff, nil
}

// GetServicePrice получает цену услуги
func (r *PostgresRepository) GetServicePrice(ctx context.Context, tariffVersionID int32, serviceID string) (*models.TariffServicePrice, error) {
	price := &models.TariffServicePrice{}
	err := r.queryRow(ctx,
		`SELECT id, tariff_version_id, service_id, included_price_rub, overage_price_rub, created_at, updated_at
		 FROM tariff_service_prices 
		 WHERE tariff_version_id = $1 AND service_id = $2`,
		tariffVersionID, serviceID,
	).Scan(&price.ID, &price.TariffVersionID, &price.ServiceID, &price.IncludedPriceRub,
		&price.OveragePriceRub, &price.CreatedAt, &price.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return price, nil
}

// CreatePaymentOrder создает заказ на оплату
func (r *PostgresRepository) CreatePaymentOrder(ctx context.Context, order *models.PaymentOrder) error {
	return r.queryRow(ctx,
		`INSERT INTO payment_orders (account_id, amount_rub, status, metadata, created_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 RETURNING id, created_at`,
		order.AccountID, order.AmountRub, order.Status, order.Metadata,
	).Scan(&order.ID, &order.CreatedAt)
}

// GetPaymentOrder получает заказ по ID
func (r *PostgresRepository) GetPaymentOrder(ctx context.Context, id int32) (*models.PaymentOrder, error) {
	order := &models.PaymentOrder{}
	err := r.queryRow(ctx,
		`SELECT id, account_id, amount_rub, status, yookassa_payment_id, metadata, created_at, paid_at
		 FROM payment_orders WHERE id = $1`, id,
	).Scan(&order.ID, &order.AccountID, &order.AmountRub, &order.Status,
		&order.YookassaPaymentID, &order.Metadata, &order.CreatedAt, &order.PaidAt)
	if err != nil {
		return nil, err
	}
	return order, nil
}

// GetPaymentOrderByYookassaID получает заказ по ID ЮКассы
func (r *PostgresRepository) GetPaymentOrderByYookassaID(ctx context.Context, paymentID string) (*models.PaymentOrder, error) {
	order := &models.PaymentOrder{}
	err := r.queryRow(ctx,
		`SELECT id, account_id, amount_rub, status, yookassa_payment_id, metadata, created_at, paid_at
		 FROM payment_orders WHERE yookassa_payment_id = $1`, paymentID,
	).Scan(&order.ID, &order.AccountID, &order.AmountRub, &order.Status,
		&order.YookassaPaymentID, &order.Metadata, &order.CreatedAt, &order.PaidAt)
	if err != nil {
		return nil, err
	}
	return order, nil
}

// UpdatePaymentOrder обновляет заказ
func (r *PostgresRepository) UpdatePaymentOrder(ctx context.Context, order *models.PaymentOrder) error {
	return r.exec(ctx,
		`UPDATE payment_orders 
		 SET status = $1, yookassa_payment_id = $2, paid_at = $3
		 WHERE id = $4`,
		order.Status, order.YookassaPaymentID, order.PaidAt, order.ID,
	)
}

// GetExpiredPendingPayments возвращает просроченные pending платежи
func (r *PostgresRepository) GetExpiredPendingPayments(ctx context.Context, timeout time.Duration) ([]*models.PaymentOrder, error) {
	cutoff := time.Now().Add(-timeout)
	
	rows, err := r.query(ctx,
		`SELECT id, account_id, amount_rub, status, yookassa_payment_id, metadata, created_at, paid_at
		 FROM payment_orders 
		 WHERE status = 'pending' AND created_at < $1`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []*models.PaymentOrder
	for rows.Next() {
		order := &models.PaymentOrder{}
		err := rows.Scan(&order.ID, &order.AccountID, &order.AmountRub, &order.Status,
			&order.YookassaPaymentID, &order.Metadata, &order.CreatedAt, &order.PaidAt)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	
	return orders, rows.Err()
}
