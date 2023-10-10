package ecom

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

type BrowserInfo struct {
	UserAgent     string `json:"user_agent" binding:"required" example:"Mozilla/5.0 (Linux; Android 9; Redmi Note 5 Build/PKQ1.180904.001; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/99.0.4844.73 Mobile Safari/537.36"`
	AcceptHeader  string `json:"accept_header" binding:"required" example:"*/*"`
	ColorDepth    *int   `json:"color_depth" binding:"required" example:"24" format:"int64"`
	IP            string `json:"ip" binding:"required" example:"127.0.0.1"`
	Language      string `json:"language" binding:"required" example:"ru-RU"`
	ScreenHeight  *int   `json:"screen_height" binding:"required" example:"786" format:"int64"`
	ScreenWidth   *int   `json:"screen_width" binding:"required" example:"393" format:"int64"`
	ScreenPrint   string `json:"screen_print" binding:"required" example:"Current Resolution: 393x786, Available Resolution: 393x786, Color Depth: 24, Device XDPI: null, Device YDPI: null"`
	TZ            *int   `json:"tz" binding:"required" example:"-180" format:"int64"`
	TimeZone      string `json:"time_zone" binding:"required" example:"Europe/Moscow"`
	JavaEnabled   *bool  `json:"java_enabled" binding:"required"`
	DeviceChannel string `json:"device_channel" binding:"required" example:"02"`
}

type ThreeDSecure10 struct {
	AcsUrl *string `json:"acs_url"`
	PaReq  *string `json:"pareq"`
}

type ThreeDSecure20 struct {
	AcsUrl *string `json:"acs_url"`
	Creq   *string `json:"creq"`
}

type ThreeDSMethodUrl struct {
	MethodUrl         *string `json:"method_url"`
	ThreeDSMethodData *string `json:"method_data"`
}

type SbpQR struct {
	QrcId   *string `json:"qrcid"`
	Payload *string `json:"payload"`
}

type Transaction struct {
	Id                  *int                   `json:"id"`
	Created             *time.Time             `json:"created"`
	Type                *string                `json:"type"`
	Status              *string                `json:"status"`
	Profile             *repository.Profile    `json:"profile"`
	Account             *Account               `json:"account"`
	Channel             *repository.Channel    `json:"channel"`
	Instrument          *repository.Instrument `json:"instrument"`
	InstrumentId        *int                   `json:"instrument_id"`
	Amount              *uint                  `json:"amount"`
	Commission          *uint                  `json:"commission"`
	Currency            *repository.Currency   `json:"currency"`
	AmountConverted     *uint                  `json:"amount_converted"`
	CommissionConverted *uint                  `json:"commission_converted"`
	CurrencyConverted   *repository.Currency   `json:"currency_converted"`
	AuthCode            *string                `json:"auth_code"`
	RRN                 *string                `json:"rrn"`
	ResponseCode        *string                `json:"response_code"`
	ErrorMessage        *string                `json:"error_message"`
	RemoteId            *string                `json:"remote_id"`
	OrderId             *string                `json:"order_id"`
	Description         *string                `json:"description"`
	Reference           *Transaction           `json:"reference"`
	SbpQR               *SbpQR                 `json:"sbpqr"`
	ThreeDSecure10      *ThreeDSecure10        `json:"threedsecure10"`
	ThreeDSecure20      *ThreeDSecure20        `json:"threedsecure20"`
	ThreeDSMethodUrl    *ThreeDSMethodUrl      `json:"threedsmethodurl"`
	AdditionalData      *AdditionalData        `json:"additional_data"`
	Customer            *string                `json:"customer"`
	BrowserInfo         *BrowserInfo           `json:"browser_info"`
	InstrumentInstance  interface{}            `json:"instrument_instance"`
}

func (tx *Transaction) SafeQrcId() string {
	var remoteId string

	if tx.SbpQR != nil {
		if tx.SbpQR.QrcId != nil {
			remoteId = *tx.SbpQR.QrcId
		}
	}

	return remoteId
}

func (tx *Transaction) SafeRemoteId() string {
	var remoteId string

	if tx.RemoteId != nil {
		remoteId = *tx.RemoteId
	}

	return remoteId
}

func (tx *Transaction) TotalAmount() uint {
	return *tx.Amount + *tx.Commission
}

func (tx *Transaction) TotalAmountConverted() uint {
	return *tx.AmountConverted + *tx.CommissionConverted
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

func (tx *Transaction) Wait3DS() {
	txStatus := repository.WAIT3DS
	tx.Status = &txStatus
}

func (tx *Transaction) Is3DSWaiting() bool {
	return *tx.Status == repository.WAIT3DS
}

func (tx *Transaction) WaitSBP() {
	txStatus := repository.WAITSBP
	tx.Status = &txStatus
}

func (tx *Transaction) IsSBPWaiting() bool {
	return *tx.Status == repository.WAITSBP
}

func (tx *Transaction) WaitMethodUrl() {
	txStatus := repository.WAITMETHODURL
	tx.Status = &txStatus
}

func (tx *Transaction) IsMethodUrlWaiting() bool {
	return *tx.Status == repository.WAITMETHODURL
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

func (tx *Transaction) IsPreAuth() bool {
	return *tx.Type == repository.PREAUTH
}

func (tx *Transaction) IsAuth() bool {
	return *tx.Type == repository.AUTH
}

func (tx *Transaction) IsRebill() bool {
	return *tx.Type == repository.REBILL
}

func (tx *Transaction) IsConfirm() bool {
	return *tx.Type == repository.CONFIRMAUTH
}

func NewTransaction(
	ctx interface{},
	instrumentInstance interface{},
	txType string,
	rateStore repository.RateRepository,
	orderId *string,
	description *string,
	profile *repository.Profile,
	account *Account,
	channel *repository.Channel,
	instrument *repository.Instrument,
	instrumentId *int,
	amount *uint,
	commission *uint,
	customer *string,
	reference *Transaction,
	browserInfo *BrowserInfo,
) (error, *Transaction) {
	var amountConverted uint
	var commissionConverted uint

	amountConverted = *amount
	commissionConverted = *commission

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
		commissionConverted = uint(math.Round(float64(*commission) * *rate))
	}

	transaction := &Transaction{
		Type:                &txType,
		Profile:             profile,
		Account:             account,
		Channel:             channel,
		Instrument:          instrument,
		InstrumentId:        instrumentId,
		Currency:            profile.Currency,
		Amount:              amount,
		Commission:          commission,
		AmountConverted:     &amountConverted,
		CommissionConverted: &commissionConverted,
		CurrencyConverted:   account.Currency,
		OrderId:             orderId,
		Description:         description,
		Reference:           reference,
		Customer:            customer,
		BrowserInfo:         browserInfo,
		InstrumentInstance:  instrumentInstance,
	}

	transaction.New()

	return nil, transaction
}

type TurnOverResult struct {
	Cnt uint
	Sum uint
}

type TransactionSpecification interface {
	ToSqlClauses() string
}

type TransactionRepository interface {
	Add(ctx interface{}, transaction *Transaction) error
	Update(ctx interface{}, transaction *Transaction) (error, bool)
	Query(ctx interface{}, specification TransactionSpecification) (error, int, []*Transaction)
	TypeTurnOver(ctx interface{}, specification TransactionSpecification) (error, *map[string]TurnOverResult)
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
			type,
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
			threedsecure10,
			threedsecure20,
			threedsmethodurl,
			error_message,
			additional_data,
			customer,
			browser_info,
			commission,
			commission_converted,
			sbpqr,
			description
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28) returning id, created`,
		transaction.Type,
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
		transaction.ThreeDSecure10,
		transaction.ThreeDSecure20,
		transaction.ThreeDSMethodUrl,
		transaction.ErrorMessage,
		transaction.AdditionalData,
		transaction.Customer,
		transaction.BrowserInfo,
		transaction.Commission,
		transaction.CommissionConverted,
		transaction.SbpQR,
		transaction.Description,
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

func (ts *PGPoolTransactionStore) TypeTurnOver(ctx interface{}, specification TransactionSpecification) (error, *map[string]TurnOverResult) {
	result := make(map[string]TurnOverResult)

	rows, err := ts.pool.Query(
		context.Background(), fmt.Sprintf(
			`select
				type,
				count(id),
				sum(amount+commission)
			from transactions %s group by type`,
			specification.ToSqlClauses(),
		),
	)

	if err != nil {
		return fmt.Errorf("failed to query type turn over rows: %v", err), &result
	}
	defer rows.Close()

	for rows.Next() {
		var opType string
		var turnOverResult TurnOverResult

		if err := rows.Scan(&opType, &turnOverResult.Cnt, &turnOverResult.Sum); err != nil {
			return fmt.Errorf("failed to get type turn over row: %v", err), &result
		}

		result[opType] = turnOverResult
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterating over rows of type turn over: %v", err), &result
	}

	return nil, &result
}

func (ts *PGPoolTransactionStore) Query(ctx interface{}, specification TransactionSpecification) (error, int, []*Transaction) {
	var l []*Transaction
	var c int = 0

	countQuery := "select count(*) from transactions"

	if strings.Contains(specification.ToSqlClauses(), "limit") {
		whereClause := specification.ToSqlClauses()[:strings.Index(specification.ToSqlClauses(), "limit")]
		countQuery = fmt.Sprintf("select count(*) from (select * from transactions %s) as foo", whereClause)
		ts.logger(ctx).Print(countQuery)
	}

	err := ts.pool.QueryRow(
		context.Background(),
		countQuery,
	).Scan(&c)

	if err != nil {
		return fmt.Errorf("failed to get transactions cnt: %v", err), c, l
	}

	rows, err := ts.pool.Query(
		context.Background(), fmt.Sprintf(
			`select
				id,
				created,
				type,
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
				threedsecure10,
				threedsecure20,
				threedsmethodurl,
				error_message,
				additional_data,
				customer,
				browser_info,
				commission,
				commission_converted,
				sbpqr,
				description
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
			&transaction.Type,
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
			&transaction.ThreeDSecure10,
			&transaction.ThreeDSecure20,
			&transaction.ThreeDSMethodUrl,
			&transaction.ErrorMessage,
			&transaction.AdditionalData,
			&transaction.Customer,
			&transaction.BrowserInfo,
			&transaction.Commission,
			&transaction.CommissionConverted,
			&transaction.SbpQR,
			&transaction.Description,
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
		l = append(l, &transaction)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterating over rows of transactions: %v", err), c, l
	}

	for _, t := range l {
		if err := ts.refreshTransactionForeigns(ctx, t); err != nil {
			return fmt.Errorf("Can not update transaction foreigns: %v", err), c, l
		}
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
			type=COALESCE($2, type),
			status=COALESCE($3, status),
			profile_id=COALESCE($4, profile_id),
			account_id=COALESCE($5, account_id),
			channel_id=COALESCE($6, channel_id),
			instrument_id=COALESCE($7, instrument_id),
			instrument=COALESCE($8, instrument),
			amount=COALESCE($9, amount),
			currency_id=COALESCE($10, currency_id),
			amount_converted=COALESCE($11, amount_converted),
			currency_converted_id=COALESCE($12, currency_converted_id),
			authcode=COALESCE($13, authcode),
			rrn=COALESCE($14, rrn),
			response_code=COALESCE($15, response_code),
			remote_id=COALESCE($16, remote_id),
			order_id=COALESCE($17, order_id),
			reference_id=COALESCE($18, reference_id),
			threedsecure10=COALESCE($19, threedsecure10),
			threedsecure20=COALESCE($20, threedsecure20),
			threedsmethodurl=COALESCE($21, threedsmethodurl),
			error_message=COALESCE($22, error_message),
			additional_data=COALESCE($23, additional_data),
			customer=COALESCE($24, customer),
			browser_info=COALESCE($25, browser_info),
			commission=COALESCE($26, commission),
			commission_converted=COALESCE($27, commission_converted),
			sbpqr=COALESCE($28, sbpqr),
			description=COALESCE($29, description)
		where
			id=$1
		returning
			type,
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
			threedsecure10,
			threedsecure20,
			threedsmethodurl,
			error_message,
			additional_data,
			customer,
			browser_info,
			commission,
			commission_converted,
			sbpqr,
			description`,
		transaction.Id,
		transaction.Type,
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
		transaction.ThreeDSecure10,
		transaction.ThreeDSecure20,
		transaction.ThreeDSMethodUrl,
		transaction.ErrorMessage,
		transaction.AdditionalData,
		transaction.Customer,
		transaction.BrowserInfo,
		transaction.Commission,
		transaction.CommissionConverted,
		transaction.SbpQR,
		transaction.Description,
	).Scan(
		&transaction.Type,
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
		&transaction.ThreeDSecure10,
		&transaction.ThreeDSecure20,
		&transaction.ThreeDSMethodUrl,
		&transaction.ErrorMessage,
		&transaction.AdditionalData,
		&transaction.Customer,
		&transaction.BrowserInfo,
		&transaction.Commission,
		&transaction.CommissionConverted,
		&transaction.SbpQR,
		&transaction.Description,
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
