package repository

import (
	"fmt"
	"time"
	"context"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type Rate struct {
	Id       *int
	Date     *time.Time
	Rate     *float64
	Nominal  *int
	Currency *Currency
}

type RateSpecification interface {
	ToSqlClauses() string
}

type RateRepository interface {
	Add(ctx interface{}, rate *Rate) error
	GetRate(ctx interface{}, date *time.Time, src *Currency, dst *Currency) (error, bool, *float64)
	Query(ctx interface{}, specification RateSpecification) (error, int, []*Rate)
}

type RateSpecificationByTimeAndCurrency struct {
	time     time.Time
	currency *Currency
}

func (rsbtac *RateSpecificationByTimeAndCurrency) ToSqlClauses() string {
	return fmt.Sprintf("where date='%s'::date and currency_id=%d", rsbtac.time.Format("2006-01-02"), *rsbtac.currency.Id)
}

func NewRateSpecificationByTimeAndCurrency(time time.Time, currency *Currency) RateSpecification {
	return &RateSpecificationByTimeAndCurrency{
		time:     time,
		currency: currency,
	}
}

type PGPoolRateStore struct {
	pool          *pgxpool.Pool
	currencyStore CurrencyRepository
	logger        LoggerFunc
}

func (rs *PGPoolRateStore) GetRate(ctx interface{}, date *time.Time, src *Currency, dst *Currency) (error, bool, *float64) {
	var rate *float64

	err := rs.pool.QueryRow(
		context.Background(),
		`SELECT a.rate / a.nominal / (SELECT b.rate / b.nominal
		     FROM rates AS b
		     WHERE b.date = a.date AND b.currency_id = $1) AS rate
		 FROM rates AS a
		 WHERE a.date <= $2 AND a.currency_id = $3 ORDER BY a.date DESC LIMIT 1`,
		dst.Id,
		date,
		src.Id,
	).Scan(
		&rate,
	)

	return err, err == pgx.ErrNoRows || rate == nil, rate
}

func (rs *PGPoolRateStore) Add(ctx interface{}, rate *Rate) error {
	var currencyId *int

	if rate.Currency != nil {
		currencyId = rate.Currency.Id
	}

	return rs.pool.QueryRow(
		context.Background(),
		`insert into rates (
			date,
			rate,
			nominal,
			currency_id
		) values ($1, $2, $3, $4) returning id`,
		rate.Date,
		rate.Rate,
		rate.Nominal,
		currencyId,
	).Scan(&rate.Id)
}

func (rs *PGPoolRateStore) refreshRateCurrency(ctx interface{}, rate *Rate) error {
	if !(rate.Currency != nil && rate.Currency.Id != nil) {
		return nil
	}

	err, _, currencies := rs.currencyStore.Query(ctx, NewCurrencySpecificationByID(
		*rate.Currency.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update rate currency: %v", err)
	}

	for _, currency := range currencies {
		rate.Currency = currency
	}

	return nil
}

func (rs *PGPoolRateStore) refreshRateForeigns(ctx interface{}, rate *Rate) error {
	if err := rs.refreshRateCurrency(ctx, rate); err != nil {
		return err
	}

	return nil
}

func (rs *PGPoolRateStore) Query(ctx interface{}, specification RateSpecification) (error, int, []*Rate) {
	var l []*Rate
	var c int = 0

	err := rs.pool.QueryRow(
		context.Background(),
		"select count(*) from rates",
	).Scan(&c)

	if err != nil {
		return fmt.Errorf("failed to get rates cnt: %v", err), c, l
	}

	rows, err := rs.pool.Query(
		context.Background(), fmt.Sprintf(
			`select
				id,
				date,
				rate,
				nominal,
				currency_id
			from rates %s`,
			specification.ToSqlClauses(),
		),
	)

	if err != nil {
		return fmt.Errorf("failed to query rates rows: %v", err), c, l
	}
	defer rows.Close()

	for rows.Next() {
		var rate Rate
		var currencyId *int

		if err = rows.Scan(
			&rate.Id,
			&rate.Date,
			&rate.Rate,
			&rate.Nominal,
			&currencyId,
		); err != nil {
			return fmt.Errorf("failed to get rate row: %v", err), c, l
		}
		if currencyId != nil {
			rate.Currency = &Currency{
				Id: currencyId,
			}
		}
		l = append(l, &rate)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterating over rows of rates: %v", err), c, l
	}

	for _, r := range l {
		if err := rs.refreshRateForeigns(ctx, r); err != nil {
			return fmt.Errorf("Can not update rate foreigns: %v", err), c, l
		}
	}

	return nil, c, l
}

func NewPGPoolRateStore(
	pool          *pgxpool.Pool,
	currencyStore CurrencyRepository,
	logger        LoggerFunc,
) RateRepository {
	return &PGPoolRateStore{
		pool:          pool,
		currencyStore: currencyStore,
		logger:        logger,
	}
}
