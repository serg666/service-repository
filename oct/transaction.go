package oct

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/serg666/service-repository"
)

type AdditionalData map[string]interface{}

type Transaction struct {
	Id                 *int                   `json:"id"`
	Created            *time.Time             `json:"created"`
	Status             *string                `json:"status"`
	Profile            *repository.Profile    `json:"profile"`
	Account            *Account               `json:"account"`
	Channel            *repository.Channel    `json:"channel"`
	Instrument         *repository.Instrument `json:"instrument"`
	InstrumentId       *int                   `json:"instrument_id"`
	Amount             *uint                  `json:"amount"`
	Currency           *repository.Currency   `json:"currency"`
	AmountConverted    *uint                  `json:"amount_converted"`
	CurrencyConverted  *repository.Currency   `json:"currency_converted"`
	AuthCode           *string                `json:"auth_code"`
	RRN                *string                `json:"rrn"`
	ResponseCode       *string                `json:"response_code"`
	ErrorMessage       *string                `json:"error_message"`
	RemoteId           *string                `json:"remote_id"`
	OrderId            *string                `json:"order_id"`
	Reference          *Transaction           `json:"reference"`
	AdditionalData     *AdditionalData        `json:"additional_data"`
	Customer           *string                `json:"customer"`
	IP                 *string                `json:"ip"`
	InstrumentInstance interface{}            `json:"instrument_instance"`
}

func (tx *Transaction) TotalAmount() uint {
	return *tx.Amount
}

func (tx *Transaction) TotalAmountConverted() uint {
	return *tx.AmountConverted
}

func (tx *Transaction) New() {
	txStatus := repository.NEW
	tx.Status = &txStatus
}

func (tx *Transaction) Success() {
	txStatus := repository.SUCCESS
	tx.Status = &txStatus
}

func (tx *Transaction) IsSuccess() bool {
	return *tx.Status == repository.SUCCESS
}

func (tx *Transaction) Declined(errorMsg *string) {
	txStatus := repository.DECLINED
	tx.Status = &txStatus
	tx.ErrorMessage = errorMsg
}

func (tx *Transaction) InFinalState() bool {
	switch *tx.Status {
	case
		repository.SUCCESS,
		repository.DECLINED:
		return true
	}
	return false
}

func NewTransaction(
	ctx interface{},
	InstrumentInstance interface{},
	rateStore repository.RateRepository,
	orderId *string,
	profile *repository.Profile,
	account *Account,
	channel *repository.Channel,
	instrument *repository.Instrument,
	instrumentId *int,
	amount *uint,
	customer *string,
	ip *string,
	reference *Transaction,
) (error, *Transaction) {
	var amountConverted uint

	amountConverted = *amount

	if *profile.Currency.Id != *account.Currency.Id {
		if !*account.CurrencyConversionEnabled {
			return errors.New("currency conversion disabled"), nil
		}

		now := time.Now()
		err, rateNotFound, rate := rateStore.GetRate(ctx, &now, profile.Currency, account.Currency)

		if err != nil {
			return fmt.Errorf("failed to get currency convertion rate: %v", err), nil
		}

		if rateNotFound {
			return errors.New("currency conversion rate not found"), nil
		}

		amountConverted = uint(math.Round(float64(*amount) * *rate))
	}

	transaction := &Transaction{
		Profile:            profile,
		Account:            account,
		Channel:            channel,
		Instrument:         instrument,
		InstrumentId:       instrumentId,
		Currency:           profile.Currency,
		Amount:             amount,
		AmountConverted:    &amountConverted,
		CurrencyConverted:  account.Currency,
		OrderId:            orderId,
		Reference:          reference,
		Customer:           customer,
		IP:                 ip,
		InstrumentInstance: InstrumentInstance,
	}

	transaction.New()

	return nil, transaction
}

type TransactionSpecification interface {
	ToSqlClauses() string
}

type TransactionRepository interface {
	Add(ctx interface{}, transaction *Transaction) error
	Update(ctx interface{}, transaction *Transaction) (error, bool)
	Query(ctx interface{}, specification TransactionSpecification) (error, int, []*Transaction)
}

type TransactionSpecificationWithLimitAndOffset struct {
	limit  int
	offset int
}

func (tswlao *TransactionSpecificationWithLimitAndOffset) ToSqlClauses() string {
	return fmt.Sprintf("limit %d offset %d", tswlao.limit, tswlao.offset)
}

type TransactionSpecificationByID struct {
	id int
}

func (tsbyid *TransactionSpecificationByID) ToSqlClauses() string {
	return fmt.Sprintf("where id=%d", tsbyid.id)
}

type TransactionSpecificationByReferenceIdAndStatus struct {
	id     int
	status string
}

func (spec *TransactionSpecificationByReferenceIdAndStatus) ToSqlClauses() string {
	return fmt.Sprintf("where reference_id=%d and status='%s'", spec.id, spec.status)
}

type TransactionSpecificationByNotFinalStatus struct {
	limit  int
	lastid int
}

func (spec *TransactionSpecificationByNotFinalStatus) ToSqlClauses() string {
	return fmt.Sprintf("where id > %d and remote_id is not null and status not in ('%s', '%s') order by id asc limit %d",
		spec.lastid,
		repository.SUCCESS,
		repository.DECLINED,
		spec.limit,
	)
}

func NewTransactionSpecificationByNotFinalStatus(limit int, lastid int) TransactionSpecification {
	return &TransactionSpecificationByNotFinalStatus{
		limit:  limit,
		lastid: lastid,
	}
}

func NewTransactionSpecificationByID(id int) TransactionSpecification {
	return &TransactionSpecificationByID{id: id}
}

func NewTransactionSpecificationWithLimitAndOffset(limit int, offset int) TransactionSpecification {
	return &TransactionSpecificationWithLimitAndOffset{
		limit:  limit,
		offset: offset,
	}
}

func NewTransactionSpecificationByReferenceIdAndStatus(id int, status string) TransactionSpecification {
	return &TransactionSpecificationByReferenceIdAndStatus{
		id:     id,
		status: status,
	}
}

// Deprecated
type TransactionSpecificationByOrderIdAndProfileIdWithLimitAndOffset struct {
	orderId        string
	profileId      int
	limitAndOffset TransactionSpecificationWithLimitAndOffset
}

// Deprecated
func (spec *TransactionSpecificationByOrderIdAndProfileIdWithLimitAndOffset) ToSqlClauses() string {
	var conditions []string
	if spec.profileId != 0 {
		conditions = append(conditions, fmt.Sprintf("profile_id=%d", spec.profileId))
	}
	if spec.orderId != "" {
		conditions = append(conditions, fmt.Sprintf("order_id='%s'", spec.orderId))
	}

	query := ""
	if len(conditions) > 0 {
		query = fmt.Sprintf("where %s", strings.Join(conditions, " AND "))
	}

	return fmt.Sprintf("%s %s", query, spec.limitAndOffset.ToSqlClauses())
}

// Deprecated
func NewTransactionSpecificationByOrderIdAndProfileIdWithLimitAndOffset(
	orderId string,
	profileId int,
	limit int,
	offset int,
) TransactionSpecification {
	return &TransactionSpecificationByOrderIdAndProfileIdWithLimitAndOffset{
		profileId: profileId,
		orderId:   orderId,
		limitAndOffset: TransactionSpecificationWithLimitAndOffset{
			limit:  limit,
			offset: offset,
		},
	}
}

type SpecWhere struct {
	conditions []string
}

func (s SpecWhere) ToSqlClauses() string {
	if len(s.conditions) == 0 {
		return ""
	}
	return fmt.Sprintf("WHERE %s", strings.Join(s.conditions, " AND "))
}

func (s *SpecWhere) addCondition(row string) {
	s.conditions = append(s.conditions, row)
}

func (s *SpecWhere) clean() {
	s.conditions = []string{}
}

type SpecPaginate struct {
	Limit  int
	Offset int
}

func (s SpecPaginate) ToSqlClauses() string {
	return fmt.Sprintf("LIMIT %d OFFSET %d", s.Limit, s.Offset)
}

type TransactionFilters struct {
	where          SpecWhere
	Paginate       SpecPaginate
	TransactionIds []string
	ProfileIds     []string
	OrderId        string
	RRN            string
	AuthCode       string
	RemoteId       string
	CardMask       string
}

func (s *TransactionFilters) ToSqlClauses() string {
	defer s.where.clean()

	if len(s.ProfileIds) > 0 {
		s.where.addCondition(fmt.Sprintf("profile_id IN (%s)", strings.Join(s.ProfileIds, ",")))
	}
	if len(s.TransactionIds) > 0 {
		s.where.addCondition(fmt.Sprintf("id IN (%s)", strings.Join(s.TransactionIds, ",")))
	}
	if s.OrderId != "" {
		s.where.addCondition(fmt.Sprintf("order_id = '%s'", s.OrderId))
	}
	if s.RRN != "" {
		s.where.addCondition(fmt.Sprintf("TRIM(rrn) = '%s'", s.RRN))
	}
	if s.AuthCode != "" {
		s.where.addCondition(fmt.Sprintf("authcode = '%s'", s.AuthCode))
	}
	if s.RemoteId != "" {
		s.where.addCondition(fmt.Sprintf("remote_id = '%s'", s.RemoteId))
	}
	if s.CardMask != "" {
		subSql := fmt.Sprintf("SELECT cm.id FROM cardmasks cm WHERE mask LIKE '%%%s'", s.CardMask)
		s.where.addCondition(fmt.Sprintf("instrument IN (%s)", subSql))
	}
	return fmt.Sprintf("%s %s", s.where.ToSqlClauses(), s.Paginate.ToSqlClauses())
}

type PGPoolTransactionStore struct {
	pool            *pgxpool.Pool
	profileStore    repository.ProfileRepository
	instrumentStore repository.InstrumentRepository
	accountStore    AccountRepository
	channelStore    repository.ChannelRepository
	currencyStore   repository.CurrencyRepository
	logger          repository.LoggerFunc
}

func (ts *PGPoolTransactionStore) Add(ctx interface{}, transaction *Transaction) error {
	var profileId *int
	var accountId *int
	var channelId *int
	var instrumentId *int
	var currencyId *int
	var currencyConvertedId *int
	var referenceId *int

	if transaction.Profile != nil {
		profileId = transaction.Profile.Id
	}

	if transaction.Account != nil {
		accountId = transaction.Account.Id
	}

	if transaction.Channel != nil {
		channelId = transaction.Channel.Id
	}

	if transaction.Instrument != nil {
		instrumentId = transaction.Instrument.Id
	}

	if transaction.Currency != nil {
		currencyId = transaction.Currency.Id
	}

	if transaction.CurrencyConverted != nil {
		currencyConvertedId = transaction.CurrencyConverted.Id
	}

	if transaction.Reference != nil {
		referenceId = transaction.Reference.Id
	}

	return ts.pool.QueryRow(
		context.Background(),
		`insert into transactions (
			status,
			profile_id,
			account_id,
			channel_id,
			instrument_id,
			instrument,
			amount,
			currency_id,
			amount_converted,
			currency_converted_id,
			authcode,
			rrn,
			response_code,
			remote_id,
			order_id,
			reference_id,
			error_message,
			additional_data,
			customer,
			ip
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20) returning id, created`,
		transaction.Status,
		profileId,
		accountId,
		channelId,
		instrumentId,
		transaction.InstrumentId,
		transaction.Amount,
		currencyId,
		transaction.AmountConverted,
		currencyConvertedId,
		transaction.AuthCode,
		transaction.RRN,
		transaction.ResponseCode,
		transaction.RemoteId,
		transaction.OrderId,
		referenceId,
		transaction.ErrorMessage,
		transaction.AdditionalData,
		transaction.Customer,
		transaction.IP,
	).Scan(&transaction.Id, &transaction.Created)
}

func (ts *PGPoolTransactionStore) refreshTransactionProfile(ctx interface{}, transaction *Transaction) error {
	if !(transaction.Profile != nil && transaction.Profile.Id != nil) {
		return nil
	}

	err, _, profiles := ts.profileStore.Query(ctx, repository.NewProfileSpecificationByID(
		*transaction.Profile.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update transaction profile: %v", err)
	}

	for _, profile := range profiles {
		transaction.Profile = profile
	}

	return nil
}

func (ts *PGPoolTransactionStore) refreshTransactionAccount(ctx interface{}, transaction *Transaction) error {
	if !(transaction.Account != nil && transaction.Account.Id != nil) {
		return nil
	}

	err, _, accounts := ts.accountStore.Query(ctx, NewAccountSpecificationByID(
		*transaction.Account.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update transaction account: %v", err)
	}

	for _, account := range accounts {
		transaction.Account = account
	}

	return nil
}

func (ts *PGPoolTransactionStore) refreshTransactionChannel(ctx interface{}, transaction *Transaction) error {
	if !(transaction.Channel != nil && transaction.Channel.Id != nil) {
		return nil
	}

	err, _, channels := ts.channelStore.Query(ctx, repository.NewChannelSpecificationByID(
		*transaction.Channel.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update transaction channel: %v", err)
	}

	for _, channel := range channels {
		transaction.Channel = channel
	}

	return nil
}

func (ts *PGPoolTransactionStore) refreshTransactionInstrument(ctx interface{}, transaction *Transaction) error {
	if !(transaction.Instrument != nil && transaction.Instrument.Id != nil) {
		return nil
	}

	err, _, instruments := ts.instrumentStore.Query(ctx, repository.NewInstrumentSpecificationByID(
		*transaction.Instrument.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update transaction instrument: %v", err)
	}

	for _, instrument := range instruments {
		transaction.Instrument = instrument
	}

	return nil
}

func (ts *PGPoolTransactionStore) refreshTransactionCurrency(ctx interface{}, transaction *Transaction) error {
	if !(transaction.Currency != nil && transaction.Currency.Id != nil) {
		return nil
	}

	err, _, currencies := ts.currencyStore.Query(ctx, repository.NewCurrencySpecificationByID(
		*transaction.Currency.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update transaction currency: %v", err)
	}

	for _, currency := range currencies {
		transaction.Currency = currency
	}

	return nil
}

func (ts *PGPoolTransactionStore) refreshTransactionCurrencyConverted(ctx interface{}, transaction *Transaction) error {
	if !(transaction.CurrencyConverted != nil && transaction.CurrencyConverted.Id != nil) {
		return nil
	}

	err, _, currencies := ts.currencyStore.Query(ctx, repository.NewCurrencySpecificationByID(
		*transaction.CurrencyConverted.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update transaction currency converted: %v", err)
	}

	for _, currency := range currencies {
		transaction.CurrencyConverted = currency
	}

	return nil
}

func (ts *PGPoolTransactionStore) refreshTransactionReference(ctx interface{}, transaction *Transaction) error {
	if !(transaction.Reference != nil && transaction.Reference.Id != nil) {
		return nil
	}

	err, _, transactions := ts.Query(ctx, NewTransactionSpecificationByID(
		*transaction.Reference.Id,
	))

	if err != nil {
		return fmt.Errorf("Can not update transaction reference: %v", err)
	}

	for _, tx := range transactions {
		transaction.Reference = tx
	}

	return nil
}

func (ts *PGPoolTransactionStore) refreshTransactionForeigns(ctx interface{}, transaction *Transaction) error {
	if err := ts.refreshTransactionProfile(ctx, transaction); err != nil {
		return err
	}

	if err := ts.refreshTransactionAccount(ctx, transaction); err != nil {
		return err
	}

	if err := ts.refreshTransactionChannel(ctx, transaction); err != nil {
		return err
	}

	if err := ts.refreshTransactionInstrument(ctx, transaction); err != nil {
		return err
	}

	if err := ts.refreshTransactionCurrency(ctx, transaction); err != nil {
		return err
	}

	if err := ts.refreshTransactionCurrencyConverted(ctx, transaction); err != nil {
		return err
	}

	if err := ts.refreshTransactionReference(ctx, transaction); err != nil {
		return err
	}

	return nil
}

func (ts *PGPoolTransactionStore) Query(ctx interface{}, specification TransactionSpecification) (error, int, []*Transaction) {
	var l []*Transaction
	var c int = 0

	conn, err := ts.pool.Acquire(context.Background())

	if err != nil {
		return fmt.Errorf("failed to acquire connection from the pool: %v", err), c, l
	}
	defer conn.Release()

	countQuery := "select count(*) from transactions"

	if strings.Contains(specification.ToSqlClauses(), "limit") {
		whereClause := specification.ToSqlClauses()[:strings.Index(specification.ToSqlClauses(), "limit")]
		countQuery = fmt.Sprintf("select count(*) from (select * from transactions %s) as foo", whereClause)
		ts.logger(ctx).Print(countQuery)
	}

	err = conn.QueryRow(
		context.Background(),
		countQuery,
	).Scan(&c)

	if err != nil {
		return fmt.Errorf("failed to get transactions cnt: %v", err), c, l
	}

	rows, err := conn.Query(
		context.Background(), fmt.Sprintf(
			`select
				id,
				created,
				status,
				profile_id,
				account_id,
				channel_id,
				instrument_id,
				instrument,
				amount,
				currency_id,
				amount_converted,
				currency_converted_id,
				authcode,
				rrn,
				response_code,
				remote_id,
				order_id,
				reference_id,
				error_message,
				additional_data,
				customer,
				ip
			from transactions %s`,
			specification.ToSqlClauses(),
		),
	)

	if err != nil {
		return fmt.Errorf("failed to query transactions rows: %v", err), c, l
	}
	defer rows.Close()

	for rows.Next() {
		var transaction Transaction
		var profileId *int
		var accountId *int
		var channelId *int
		var instrumentId *int
		var currencyId *int
		var currencyConvertedId *int
		var referenceId *int

		if err = rows.Scan(
			&transaction.Id,
			&transaction.Created,
			&transaction.Status,
			&profileId,
			&accountId,
			&channelId,
			&instrumentId,
			&transaction.InstrumentId,
			&transaction.Amount,
			&currencyId,
			&transaction.AmountConverted,
			&currencyConvertedId,
			&transaction.AuthCode,
			&transaction.RRN,
			&transaction.ResponseCode,
			&transaction.RemoteId,
			&transaction.OrderId,
			&referenceId,
			&transaction.ErrorMessage,
			&transaction.AdditionalData,
			&transaction.Customer,
			&transaction.IP,
		); err != nil {
			return fmt.Errorf("failed to get transaction row: %v", err), c, l
		}
		if profileId != nil {
			transaction.Profile = &repository.Profile{
				Id: profileId,
			}
		}
		if accountId != nil {
			transaction.Account = &Account{
				Id: accountId,
			}
		}
		if channelId != nil {
			transaction.Channel = &repository.Channel{
				Id: channelId,
			}
		}
		if instrumentId != nil {
			transaction.Instrument = &repository.Instrument{
				Id: instrumentId,
			}
		}
		if currencyId != nil {
			transaction.Currency = &repository.Currency{
				Id: currencyId,
			}
		}
		if currencyConvertedId != nil {
			transaction.CurrencyConverted = &repository.Currency{
				Id: currencyConvertedId,
			}
		}
		if referenceId != nil {
			transaction.Reference = &Transaction{
				Id: referenceId,
			}
		}
		if err := ts.refreshTransactionForeigns(ctx, &transaction); err != nil {
			return fmt.Errorf("Can not update transaction foreigns: %v", err), c, l
		}
		l = append(l, &transaction)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterating over rows of transactions: %v", err), c, l
	}

	return nil, c, l
}

func (ts *PGPoolTransactionStore) Update(ctx interface{}, transaction *Transaction) (error, bool) {
	var profileId *int
	var accountId *int
	var channelId *int
	var instrumentId *int
	var currencyId *int
	var currencyConvertedId *int
	var referenceId *int

	if transaction.Profile != nil {
		profileId = transaction.Profile.Id
	}

	if transaction.Account != nil {
		accountId = transaction.Account.Id
	}

	if transaction.Channel != nil {
		channelId = transaction.Channel.Id
	}

	if transaction.Instrument != nil {
		instrumentId = transaction.Instrument.Id
	}

	if transaction.Currency != nil {
		currencyId = transaction.Currency.Id
	}

	if transaction.CurrencyConverted != nil {
		currencyConvertedId = transaction.CurrencyConverted.Id
	}

	if transaction.Reference != nil {
		referenceId = transaction.Reference.Id
	}

	err := ts.pool.QueryRow(
		context.Background(),
		`update transactions set
			status=COALESCE($2, status),
			profile_id=COALESCE($3, profile_id),
			account_id=COALESCE($4, account_id),
			channel_id=COALESCE($5, channel_id),
			instrument_id=COALESCE($6, instrument_id),
			instrument=COALESCE($7, instrument),
			amount=COALESCE($8, amount),
			currency_id=COALESCE($9, currency_id),
			amount_converted=COALESCE($10, amount_converted),
			currency_converted_id=COALESCE($11, currency_converted_id),
			authcode=COALESCE($12, authcode),
			rrn=COALESCE($13, rrn),
			response_code=COALESCE($14, response_code),
			remote_id=COALESCE($15, remote_id),
			order_id=COALESCE($16, order_id),
			reference_id=COALESCE($17, reference_id),
			error_message=COALESCE($18, error_message),
			additional_data=COALESCE($19, additional_data),
			customer=COALESCE($20, customer),
			ip=COALESCE($21, ip)
		where
			id=$1
		returning
			status,
			profile_id,
			account_id,
			channel_id,
			instrument_id,
			instrument,
			amount,
			currency_id,
			amount_converted,
			currency_converted_id,
			authcode,
			rrn,
			response_code,
			remote_id,
			order_id,
			reference_id,
			error_message,
			additional_data,
			customer,
			ip`,
		transaction.Id,
		transaction.Status,
		profileId,
		accountId,
		channelId,
		instrumentId,
		transaction.InstrumentId,
		transaction.Amount,
		currencyId,
		transaction.AmountConverted,
		currencyConvertedId,
		transaction.AuthCode,
		transaction.RRN,
		transaction.ResponseCode,
		transaction.RemoteId,
		transaction.OrderId,
		referenceId,
		transaction.ErrorMessage,
		transaction.AdditionalData,
		transaction.Customer,
		transaction.IP,
	).Scan(
		&transaction.Status,
		&profileId,
		&accountId,
		&channelId,
		&instrumentId,
		&transaction.InstrumentId,
		&transaction.Amount,
		&currencyId,
		&transaction.AmountConverted,
		&currencyConvertedId,
		&transaction.AuthCode,
		&transaction.RRN,
		&transaction.ResponseCode,
		&transaction.RemoteId,
		&transaction.OrderId,
		&referenceId,
		&transaction.ErrorMessage,
		&transaction.AdditionalData,
		&transaction.Customer,
		&transaction.IP,
	)

	if profileId != nil {
		transaction.Profile = &repository.Profile{
			Id: profileId,
		}
	}
	if accountId != nil {
		transaction.Account = &Account{
			Id: accountId,
		}
	}
	if channelId != nil {
		transaction.Channel = &repository.Channel{
			Id: channelId,
		}
	}
	if instrumentId != nil {
		transaction.Instrument = &repository.Instrument{
			Id: instrumentId,
		}
	}
	if currencyId != nil {
		transaction.Currency = &repository.Currency{
			Id: currencyId,
		}
	}
	if currencyConvertedId != nil {
		transaction.CurrencyConverted = &repository.Currency{
			Id: currencyConvertedId,
		}
	}
	if referenceId != nil {
		transaction.Reference = &Transaction{
			Id: referenceId,
		}
	}

	if e := ts.refreshTransactionForeigns(ctx, transaction); e != nil {
		return fmt.Errorf("Can not update transaction foreigns: %v", e), err == pgx.ErrNoRows
	}

	return err, err == pgx.ErrNoRows
}

func NewPGPoolTransactionStore(
	pool *pgxpool.Pool,
	profileStore repository.ProfileRepository,
	instrumentStore repository.InstrumentRepository,
	accountStore AccountRepository,
	channelStore repository.ChannelRepository,
	currencyStore repository.CurrencyRepository,
	logger repository.LoggerFunc,
) TransactionRepository {
	return &PGPoolTransactionStore{
		pool:            pool,
		profileStore:    profileStore,
		instrumentStore: instrumentStore,
		accountStore:    accountStore,
		channelStore:    channelStore,
		currencyStore:   currencyStore,
		logger:          logger,
	}
}
