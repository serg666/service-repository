package repository

import (
	"fmt"
	"time"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

const CARD_MASK_EXPIRE_DATE_FORMAT = "06/01"

type CardExpDate time.Time

func (t CardExpDate) AsTime() time.Time {
	return time.Time(t)
}

func (t *CardExpDate) MarshalJSON() ([]byte, error) {
	expire := t.AsTime()
	return json.Marshal(expire.Format(CARD_MASK_EXPIRE_DATE_FORMAT))
}

type CardMask struct {
	Id      *int         `json:"id"`
	CardID  *int         `json:"card_id"`
	Mask    *string      `json:"mask"`
	Token   *string      `json:"token"`
	Holder  *string      `json:"holder"`
	ExpDate *CardExpDate `json:"exp_date"`
}

type CardMaskSpecification interface {
	ToSqlClauses() string
}

type CardMaskRepository interface {
	Add(ctx interface{}, cardMask *CardMask) error
	Delete(ctx interface{}, cardMask *CardMask) (error, bool)
	Update(ctx interface{}, cardMask *CardMask) (error, bool)
	Query(ctx interface{}, specification CardMaskSpecification) (error, int, []*CardMask)
}

type CardMaskSpecificationWithLimitAndOffset struct {
	limit int
	offset int
}

func (cswlao *CardMaskSpecificationWithLimitAndOffset) ToSqlClauses() string {
	return fmt.Sprintf("limit %d offset %d", cswlao.limit, cswlao.offset)
}

type CardMaskSpecificationByID struct {
	id int
}

func (csbyid *CardMaskSpecificationByID) ToSqlClauses() string {
	return fmt.Sprintf("where id=%d", csbyid.id)
}

func NewCardMaskSpecificationByID(id int) CardMaskSpecification {
	return &CardMaskSpecificationByID{id: id}
}

type CardMaskSpecificationByCardID struct {
	cardId int
}

func (csbyid *CardMaskSpecificationByCardID) ToSqlClauses() string {
	return fmt.Sprintf("where card_id=%d", csbyid.cardId)
}

func NewCardMaskSpecificationByCardID(cardId int) CardMaskSpecification {
	return &CardMaskSpecificationByCardID{cardId: cardId}
}

type CardMaskSpecificationByToken struct {
	token string
}

func (csbyid *CardMaskSpecificationByToken) ToSqlClauses() string {
	return fmt.Sprintf("where token='%s'", csbyid.token)
}

func NewCardMaskSpecificationByToken(token string) CardMaskSpecification {
	return &CardMaskSpecificationByToken{token: token}
}

func NewCardMaskSpecificationWithLimitAndOffset(limit int, offset int) CardMaskSpecification {
	return &CardMaskSpecificationWithLimitAndOffset{
		limit:  limit,
		offset: offset,
	}
}

type PGPoolCardMaskStore struct {
	pool   *pgxpool.Pool
	logger LoggerFunc
}

func (cs *PGPoolCardMaskStore) Add(ctx interface{}, cardMask *CardMask) error {
	return cs.pool.QueryRow(
		context.Background(),
		"insert into cardmasks (card_id, mask, token, holder, exp_date) values ($1, $2, $3, $4, $5) returning id",
		cardMask.CardID,
		cardMask.Mask,
		cardMask.Token,
		cardMask.Holder,
		cardMask.ExpDate,
	).Scan(&cardMask.Id)
}

func (cs *PGPoolCardMaskStore) Delete(ctx interface{}, cardMask *CardMask) (error, bool) {
	var exp_dte *time.Time

	err := cs.pool.QueryRow(
		context.Background(),
		"delete from cardmasks where id=$1 returning card_id, mask, token, holder, exp_date",
		cardMask.Id,
	).Scan(
		&cardMask.CardID,
		&cardMask.Mask,
		&cardMask.Token,
		&cardMask.Holder,
		&exp_dte,
	)

	if exp_dte != nil {
		expire := CardExpDate(*exp_dte)
		cardMask.ExpDate = &expire
	}

	return err, err == pgx.ErrNoRows
}

func (cs *PGPoolCardMaskStore) Query(ctx interface{}, specification CardMaskSpecification) (error, int, []*CardMask) {
	var l []*CardMask
	var c int = 0

	conn, err := cs.pool.Acquire(context.Background())

	if err != nil {
		return fmt.Errorf("failed to acquire connection from the pool: %v", err), c, l
	}
	defer conn.Release()

	err = conn.QueryRow(
		context.Background(),
		"select count(*) from cardmasks",
	).Scan(&c)

	if err != nil {
		return fmt.Errorf("failed to get cardmasks cnt: %v", err), c, l
	}

	rows, err := conn.Query(
		context.Background(), fmt.Sprintf(
			"select id, card_id, mask, token, holder, exp_date from cardmasks %s",
			specification.ToSqlClauses(),
		),
	)

	if err != nil {
		return fmt.Errorf("failed to query cardmasks rows: %v", err), c, l
	}
	defer rows.Close()

	for rows.Next() {
		var cardMask CardMask
		var exp_dte *time.Time

		if err = rows.Scan(
			&cardMask.Id,
			&cardMask.CardID,
			&cardMask.Mask,
			&cardMask.Token,
			&cardMask.Holder,
			&exp_dte,
		); err != nil {
			return fmt.Errorf("failed to get cardmask row: %v", err), c, l
		}

		if exp_dte != nil {
			expire := CardExpDate(*exp_dte)
			cardMask.ExpDate = &expire
		}

		l = append(l, &cardMask)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterating over rows of cardmasks: %v", err), c, l
	}

	return nil, c, l
}

func (cs *PGPoolCardMaskStore) Update(ctx interface{}, cardMask *CardMask) (error, bool) {
	var exp_dte *time.Time

	err := cs.pool.QueryRow(
		context.Background(),
		`update cardmasks set
			card_id=COALESCE($2, card_id),
			mask=COALESCE($3, mask),
			token=COALESCE($4, token),
			holder=COALESCE($5, holder),
			exp_date=COALESCE($6, exp_date)
		where id=$1 returning card_id, mask, token, holder, exp_date`,
		cardMask.Id,
		cardMask.CardID,
		cardMask.Mask,
		cardMask.Token,
		cardMask.Holder,
		cardMask.ExpDate,
	).Scan(
		&cardMask.CardID,
		&cardMask.Mask,
		&cardMask.Token,
		&cardMask.Holder,
		&exp_dte,
	)

	if exp_dte != nil {
		expire := CardExpDate(*exp_dte)
		cardMask.ExpDate = &expire
	}

	return err, err == pgx.ErrNoRows
}

func NewPGPoolCardMaskStore(pool *pgxpool.Pool, logger LoggerFunc) CardMaskRepository {
	return &PGPoolCardMaskStore{
		pool:      pool,
		logger:    logger,
	}
}
