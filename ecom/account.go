package ecom

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/serg666/service-repository"
	"time"
)

type AccountSettings map[string]interface{}

type Account struct {
	Id                        *int                 `json:"id"`
	IsEnabled                 *bool                `json:"is_enabled"`
	IsTest                    *bool                `json:"is_test"`
	RebillEnabled             *bool                `json:"rebill_enabled"`
	RefundEnabled             *bool                `json:"refund_enabled"`
	ReversalEnabled           *bool                `json:"reversal_enabled"`
	PartialConfirmEnabled     *bool                `json:"partial_confirm_enabled"`
	PartialReversalEnabled    *bool                `json:"partial_reversal_enabled"`
	PartialRefundEnabled      *bool                `json:"partial_refund_enabled"`
	CurrencyConversionEnabled *bool                `json:"currency_conversion_enabled"`
	Currency                  *repository.Currency `json:"currency"`
	Settings                  *AccountSettings     `json:"settings"`
	Name                      *string              `json:"name"`
	CreatedAt                 *time.Time           `json:"created_at"`
}

func (a *Account) String() string {
	return fmt.Sprintf("Account <%d>", *a.Id)
}

type AccountSpecification interface {
	ToSqlClauses() string
}

type AccountRepository interface {
	Add(ctx interface{}, account *Account) error
	Delete(ctx interface{}, account *Account) (error, bool)
	Update(ctx interface{}, account *Account) (error, bool)
	Query(ctx interface{}, specification AccountSpecification) (error, int, []*Account)
}

type AccountSpecificationWithLimitAndOffset struct {
	limit  int
	offset int
}

func (aswlao *AccountSpecificationWithLimitAndOffset) ToSqlClauses() string {
	return fmt.Sprintf("order by id desc limit %d offset %d", aswlao.limit, aswlao.offset)
}

type AccountSpecificationByID struct {
	id int
}

func (asbyid *AccountSpecificationByID) ToSqlClauses() string {
	return fmt.Sprintf("where id=%d", asbyid.id)
}

func NewAccountSpecificationByID(id int) AccountSpecification {
	return &AccountSpecificationByID{id: id}
}

func NewAccountSpecificationWithLimitAndOffset(limit int, offset int) AccountSpecification {
	return &AccountSpecificationWithLimitAndOffset{
		limit:  limit,
		offset: offset,
	}
}

type PGPoolAccountStore struct {
	pool          *pgxpool.Pool
	currencyStore repository.CurrencyRepository
	logger        repository.LoggerFunc
}

func (as *PGPoolAccountStore) Add(ctx interface{}, account *Account) error {
	var currencyId *int

	if account.Currency != nil {
		currencyId = account.Currency.Id
	}

	return as.pool.QueryRow(
		context.Background(),
		`insert into accounts (
			is_enabled,
			is_test,
			rebill_enabled,
			refund_enabled,
			reversal_enabled,
			partial_confirm_enabled,
			partial_reversal_enabled,
			partial_refund_enabled,
			currency_conversion_enabled,
			currency_id,
			settings,
			name
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) returning id, created_at`,
		account.IsEnabled,
		account.IsTest,
		account.RebillEnabled,
		account.RefundEnabled,
		account.ReversalEnabled,
		account.PartialConfirmEnabled,
		account.PartialReversalEnabled,
		account.PartialRefundEnabled,
		account.CurrencyConversionEnabled,
		currencyId,
		account.Settings,
		account.Name,
	).Scan(
		&account.Id,
		&account.CreatedAt,
	)
}

func (as *PGPoolAccountStore) refreshAccountCurrency(ctx interface{}, account *Account) error {
	if !(account.Currency != nil && account.Currency.Id != nil) {
		return nil
	}

	err, _, currencies := as.currencyStore.Query(ctx, repository.NewCurrencySpecificationByID(
		*account.Currency.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update account currency: %v", err)
	}

	for _, currency := range currencies {
		account.Currency = currency
	}

	return nil
}

func (as *PGPoolAccountStore) refreshAccountForeigns(ctx interface{}, account *Account) error {
	if err := as.refreshAccountCurrency(ctx, account); err != nil {
		return err
	}

	return nil
}

func (as *PGPoolAccountStore) Query(ctx interface{}, specification AccountSpecification) (error, int, []*Account) {
	var l []*Account
	var c int = 0

	conn, err := as.pool.Acquire(context.Background())

	if err != nil {
		return fmt.Errorf("failed to acquire connection from the pool: %v", err), c, l
	}
	defer conn.Release()

	err = conn.QueryRow(
		context.Background(),
		"select count(*) from accounts",
	).Scan(&c)

	if err != nil {
		return fmt.Errorf("failed to get accounts cnt: %v", err), c, l
	}

	rows, err := conn.Query(
		context.Background(), fmt.Sprintf(
			`select
				id,
				is_enabled,
				is_test,
				rebill_enabled,
				refund_enabled,
				reversal_enabled,
				partial_confirm_enabled,
				partial_reversal_enabled,
				partial_refund_enabled,
				currency_conversion_enabled,
				settings,
				name,
				created_at,
				currency_id
			from accounts %s`,
			specification.ToSqlClauses(),
		),
	)

	if err != nil {
		return fmt.Errorf("failed to query accounts rows: %v", err), c, l
	}
	defer rows.Close()

	for rows.Next() {
		var account Account
		var currencyId *int

		if err = rows.Scan(
			&account.Id,
			&account.IsEnabled,
			&account.IsTest,
			&account.RebillEnabled,
			&account.RefundEnabled,
			&account.ReversalEnabled,
			&account.PartialConfirmEnabled,
			&account.PartialReversalEnabled,
			&account.PartialRefundEnabled,
			&account.CurrencyConversionEnabled,
			&account.Settings,
			&account.Name,
			&account.CreatedAt,
			&currencyId,
		); err != nil {
			return fmt.Errorf("failed to get account row: %v", err), c, l
		}
		if currencyId != nil {
			account.Currency = &repository.Currency{
				Id: currencyId,
			}
		}
		if err := as.refreshAccountForeigns(ctx, &account); err != nil {
			return fmt.Errorf("Can not update account foreigns: %v", err), c, l
		}
		l = append(l, &account)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterating over rows of accounts: %v", err), c, l
	}

	return nil, c, l
}

func (as *PGPoolAccountStore) Delete(ctx interface{}, account *Account) (error, bool) {
	var currencyId *int

	err := as.pool.QueryRow(
		context.Background(),
		`delete from
			accounts
		where
			id=$1
		returning
			is_enabled,
			is_test,
			rebill_enabled,
			refund_enabled,
			reversal_enabled,
			partial_confirm_enabled,
			partial_reversal_enabled,
			partial_refund_enabled,
			currency_conversion_enabled,
			settings,
			name,
			created_at,
			currency_id`,
		account.Id,
	).Scan(
		&account.IsEnabled,
		&account.IsTest,
		&account.RebillEnabled,
		&account.RefundEnabled,
		&account.ReversalEnabled,
		&account.PartialConfirmEnabled,
		&account.PartialReversalEnabled,
		&account.PartialRefundEnabled,
		&account.CurrencyConversionEnabled,
		&account.Settings,
		&account.Name,
		&account.CreatedAt,
		&currencyId,
	)

	if currencyId != nil {
		account.Currency = &repository.Currency{
			Id: currencyId,
		}
	}

	if e := as.refreshAccountForeigns(ctx, account); e != nil {
		return fmt.Errorf("Can not update account foreigns: %v", e), err == pgx.ErrNoRows
	}

	return err, err == pgx.ErrNoRows
}

func (as *PGPoolAccountStore) Update(ctx interface{}, account *Account) (error, bool) {
	var currencyId *int

	if account.Currency != nil {
		currencyId = account.Currency.Id
	}

	err := as.pool.QueryRow(
		context.Background(),
		`update accounts set
			is_enabled=COALESCE($2, is_enabled),
			is_test=COALESCE($3, is_test),
			rebill_enabled=COALESCE($4, rebill_enabled),
			refund_enabled=COALESCE($5, refund_enabled),
			reversal_enabled=COALESCE($6, reversal_enabled),
			partial_confirm_enabled=COALESCE($7, partial_confirm_enabled),
			partial_reversal_enabled=COALESCE($8, partial_reversal_enabled),
			partial_refund_enabled=COALESCE($9, partial_refund_enabled),
			currency_conversion_enabled=COALESCE($10, currency_conversion_enabled),
			settings=COALESCE($11, settings),
			name=COALESCE($12, name),
			currency_id=COALESCE($13, currency_id)
		where
			id=$1
		returning
			is_enabled,
			is_test,
			rebill_enabled,
			refund_enabled,
			reversal_enabled,
			partial_confirm_enabled,
			partial_reversal_enabled,
			partial_refund_enabled,
			currency_conversion_enabled,
			settings,
			name,
			created_at,
			currency_id`,
		account.Id,
		account.IsEnabled,
		account.IsTest,
		account.RebillEnabled,
		account.RefundEnabled,
		account.ReversalEnabled,
		account.PartialConfirmEnabled,
		account.PartialReversalEnabled,
		account.PartialRefundEnabled,
		account.CurrencyConversionEnabled,
		account.Settings,
		account.Name,
		currencyId,
	).Scan(
		&account.IsEnabled,
		&account.IsTest,
		&account.RebillEnabled,
		&account.RefundEnabled,
		&account.ReversalEnabled,
		&account.PartialConfirmEnabled,
		&account.PartialReversalEnabled,
		&account.PartialRefundEnabled,
		&account.CurrencyConversionEnabled,
		&account.Settings,
		&account.Name,
		&account.CreatedAt,
		&currencyId,
	)

	if currencyId != nil {
		account.Currency = &repository.Currency{
			Id: currencyId,
		}
	}

	if e := as.refreshAccountForeigns(ctx, account); e != nil {
		return fmt.Errorf("Can not update account foreigns: %v", e), err == pgx.ErrNoRows
	}

	return err, err == pgx.ErrNoRows
}

func NewPGPoolAccountStore(
	pool *pgxpool.Pool,
	currencyStore repository.CurrencyRepository,
	logger repository.LoggerFunc,
) AccountRepository {
	return &PGPoolAccountStore{
		pool:          pool,
		currencyStore: currencyStore,
		logger:        logger,
	}
}
