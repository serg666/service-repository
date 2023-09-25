package repository

import (
	"fmt"
	"sync"
	"time"
	"regexp"
	"errors"
	"strconv"
	"strings"
	"bytes"
	"net/url"
	"net/http"
	"io/ioutil"
	"crypto/sha256"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/durango/go-credit-card"
	"github.com/wk8/go-ordered-map"
	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/requestid"
)

const EXPIRE_DATE_FORMAT = "06/01"

type PAN string

type ExpDate struct {
	time.Time
}

func (t *ExpDate) UnmarshalJSON(b []byte) (err error) {
	s := strings.Trim(string(b), "\"")
	t.Time, err = time.Parse(EXPIRE_DATE_FORMAT, s)
	return
}

func (s PAN) Mask() string {
	pan := string(s)
	return fmt.Sprintf("%s**%s", pan[0:6], pan[len(pan)-4:])
}

func (s PAN) String() string {
	repeat := len(s)-4
	if repeat < 0 {
		repeat = 0
	}
	last4  := s[repeat:]
	return fmt.Sprintf("%s%s", strings.Repeat("*", repeat), string(last4))
}

type Card struct {
	Id      *int     `json:"id"`
	Token   *string  `json:"token"`
	PAN     *PAN     `json:"pan"`
	ExpDate *ExpDate `json:"exp_date"`
	Holder  *string  `json:"holder"`
}

func (c Card) FormatedExpDte(format string) *string {
	if c.ExpDate == nil {
		return nil
	}

	expire := *c.ExpDate
	formated_exp_dte := expire.Format(format)
	return &formated_exp_dte
}

func (c Card) String() string {
	var expire string
	exp := c.FormatedExpDte(EXPIRE_DATE_FORMAT)
	if exp != nil {
		expire = *exp
	}
	return fmt.Sprintf("%s (%s) <%s> [%s]", *c.PAN, expire, *c.Token, c.Type())
}

func (c Card) Type() string {
	card := creditcard.Card{
		Number: string(*c.PAN),
	}

	if c.ExpDate != nil {
		expire := *c.ExpDate
		card.Month = expire.Month().String()
		card.Year = string(expire.Year())
	}

	err := card.Method()
	if err != nil {
		return "Unknown"
	}
	return card.Company.Short
}

type CardSpecification interface {
	Specified(card *Card, i int) bool
	ToQwrStr() string
}

type CardRepository interface {
	Add(ctx interface{}, card *Card) error
	Delete(ctx interface{}, card *Card) (error, bool)
	//Update(ctx interface{}, card *Card) (error, bool)
	Query(ctx interface{}, specification CardSpecification) (error, int, []*Card)
}

type CardSpecificationWithLimitAndOffset struct {
	limit int
	offset int
}

func (cswlao *CardSpecificationWithLimitAndOffset) Specified(card *Card, i int) bool {
	return i >= cswlao.offset && i < cswlao.offset + cswlao.limit
}

func (cswlao *CardSpecificationWithLimitAndOffset) ToQwrStr() string {
	params := url.Values{}
	params.Add("offset", string(cswlao.offset))
	params.Add("limit", string(cswlao.limit))

	return fmt.Sprintf("?%s", params.Encode())
}

type CardSpecificationByToken struct {
	token string
}

type CardSpecificationByPAN struct {
	pan PAN
}

type CardSpecificationByPANEXPDTEHOLDER struct {
	pan     PAN
	exp_dte ExpDate
	holder  string
}

func (csbytoken *CardSpecificationByToken) Specified(card *Card, i int) bool {
	return csbytoken.token == *card.Token
}

func (csbytoken *CardSpecificationByToken) ToQwrStr() string {
	return fmt.Sprintf("/token/%s", csbytoken.token)
}

func (c *CardSpecificationByPANEXPDTEHOLDER) Specified(card *Card, i int) bool {
	exp := *card.ExpDate
	return c.pan == *card.PAN && c.exp_dte.Equal(exp.Time) && c.holder == *card.Holder
}

func (c *CardSpecificationByPANEXPDTEHOLDER) ToQwrStr() string {
	params := url.Values{}
	params.Add("pan", string(c.pan))
	params.Add("exp_date", c.exp_dte.Format(EXPIRE_DATE_FORMAT))
	params.Add("holder", c.holder)
	params.Add("limit", "1")
	return fmt.Sprintf("?%s", params.Encode())
}

func (csbypan *CardSpecificationByPAN) Specified(card *Card, i int) bool {
	return csbypan.pan == *card.PAN
}

func (csbypan *CardSpecificationByPAN) ToQwrStr() string {
	params := url.Values{}
	params.Add("pan", string(csbypan.pan))
	params.Add("limit", "1")
	return fmt.Sprintf("?%s", params.Encode())
}

type OrderedMapCardStore struct {
	sync.Mutex

	cards  *orderedmap.OrderedMap
	nextId int
	logger LoggerFunc
}

func generateToken(size int) (error, *string) {
	h := sha256.New()
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Errorf("can not generate rand bytes: %v", err), nil
	}
	h.Write(b)
	shaStr := h.Sum(nil)
	token := hex.EncodeToString(shaStr)
	return nil, &token
}

func (cs *OrderedMapCardStore) Add(ctx interface{}, card *Card) error {
	cs.Lock()
	defer cs.Unlock()

	err, token := generateToken(32)
	if err != nil {
		return fmt.Errorf("can not generate token: %v", err)
	}

	id := cs.nextId
	card.Id = &id
	card.Token = token
	cs.cards.Set(*card.Id, *card)
	cs.nextId++

	return nil
}

func (cs *OrderedMapCardStore) Delete(ctx interface{}, card *Card) (error, bool) {
	cs.Lock()
	defer cs.Unlock()

	value, present := cs.cards.Delete(*card.Id)
	if !present {
		return fmt.Errorf("card with id=%v not found", *card.Id), true
	}

	deleted := value.(Card)
	card.Token = deleted.Token
	card.PAN = deleted.PAN
	card.ExpDate = deleted.ExpDate
	card.Holder = deleted.Holder

	return nil, false
}
/*
func (cs *OrderedMapCardStore) Update(ctx interface{}, card *Card) (error, bool) {
	cs.Lock()
	defer cs.Unlock()

	value, present := cs.cards.Get(*card.Id)
	if !present {
		return fmt.Errorf("card with id=%v not found", *card.Id), true
	}

	old := value.(Card)

	if card.Token != nil {
		old.Token = card.Token
	} else {
		card.Token = old.Token
	}

	if card.PAN != nil {
		old.PAN = card.PAN
	} else {
		card.PAN = old.PAN
	}

	if card.ExpDate != nil {
		old.ExpDate = card.ExpDate
	} else {
		card.ExpDate = old.ExpDate
	}

	if card.Holder != nil {
		old.Holder = card.Holder
	} else {
		card.Holder = old.Holder
	}

	cs.cards.Set(*old.Id, old)

	return nil, false
}
*/
func (cs *OrderedMapCardStore) Query(ctx interface{}, specification CardSpecification) (error, int, []*Card) {
	cs.Lock()
	defer cs.Unlock()

	var l []*Card
	var c int = 0

	for el := cs.cards.Oldest(); el != nil; el = el.Next() {
		card := el.Value.(Card)
		if specification.Specified(&card, c) {
			l = append(l, &card)
		}
		c++
	}

	return nil, cs.cards.Len(), l
}

func NewOrderedMapCardStore(
	cards *orderedmap.OrderedMap,
	logger LoggerFunc,
) CardRepository {
	return &OrderedMapCardStore{
		cards:  cards,
		nextId: 1,
		logger: logger,
	}
}

func NewCardSpecificationByToken(token string) CardSpecification {
	return &CardSpecificationByToken{
		token: token,
	}
}

func NewCardSpecificationByPAN(pan PAN) CardSpecification {
	return &CardSpecificationByPAN{
		pan: pan,
	}
}

func NewCardSpecificationByPANEXPDTEHOLDER(pan PAN, exp_dte ExpDate, holder string) CardSpecification {
	return &CardSpecificationByPANEXPDTEHOLDER{
		pan:     pan,
		exp_dte: exp_dte,
		holder:  holder,
	}
}

func NewCardSpecificationWithLimitAndOffset(limit int, offset int) CardSpecification {
	return &CardSpecificationWithLimitAndOffset{
		limit:  limit,
		offset: offset,
	}
}

type HttpClientCardStore struct {
	url    string
	client *http.Client
	logger LoggerFunc
}

func (cs *HttpClientCardStore) maskParams(data string) string {
	sampleRegexp := regexp.MustCompile(`pan=[^&]+([^&]{4})`)
	result := sampleRegexp.ReplaceAllString(data, "pan=******$1")
	sampleRegexp = regexp.MustCompile(`"pan":"[^"]+([^"]{4})"`)
	result = sampleRegexp.ReplaceAllString(result, "\"pan\":\"******$1\"")
	return result
}

func (cs *HttpClientCardStore) makeRequest(
	ctx interface{},
	method string,
	uri string,
	contentType string,
	data string,
) (error, *map[string]interface{}, *int) {
	url := fmt.Sprintf("%s/%s", cs.url, uri)
	cs.logger(ctx).Printf("Requesting: %s", cs.maskParams(url))
	cs.logger(ctx).Printf("Params: %s", cs.maskParams(data))
	r, err := http.NewRequest(method, url, strings.NewReader(data))
	if err != nil {
		return fmt.Errorf("can not make new request: %v", err), nil, nil
	}

	r.Header.Add("Content-Type", contentType)
	r.Header.Add("Content-Length", strconv.Itoa(len(data)))

	if ctx != nil {
		if ginContext, ok := ctx.(*gin.Context); ok {
			if rid := requestid.Get(ginContext); len(rid) > 0 {
				r.Header.Add("X-Request-ID", rid)
			}
		}
	}

	res, err := cs.client.Do(r)
	if err != nil {
		return fmt.Errorf("can not do request: %v", err), nil, nil
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("can not read body: %v", err), nil, nil
	}

	cs.logger(ctx).Printf("response body: %s", cs.maskParams(string(body)))

	var jsonResp map[string]interface{}
	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return fmt.Errorf("can not unmarshal body: %v", err), nil, nil
	}

	return nil, &jsonResp, &res.StatusCode
}

func (cs *HttpClientCardStore) Add(ctx interface{}, card *Card) error {
	qwr := make(map[string]string)

	if card.PAN != nil {
		pan := *card.PAN
		qwr["pan"] = string(pan)
	}

	if card.ExpDate != nil {
		expire := *card.ExpDate
		qwr["exp_date"] = expire.Format(EXPIRE_DATE_FORMAT)
	}

	if card.Holder != nil {
		qwr["holder"] = *card.Holder
	}

	jsonbody, err := json.Marshal(qwr)
	if err != nil {
		return fmt.Errorf("can not marshal add card request body: %v", err)
	}

	err, jsonResp, _ := cs.makeRequest(ctx, "POST", "v1/cards", "application/json; charset=utf-8", string(jsonbody))
	if err != nil {
		return fmt.Errorf("can not make add card request: %v", err)
	}

	jsonbody, err = json.Marshal(jsonResp)
	if err != nil {
		return fmt.Errorf("can not marshal add card json response: %v", err)
	}

	d := json.NewDecoder(bytes.NewReader(jsonbody))
	d.DisallowUnknownFields()
	if err := d.Decode(card); err != nil {
		return fmt.Errorf("can not decode add card json body response: %v", err)
	}

	return nil
}

func (cs *HttpClientCardStore) Delete(ctx interface{}, card *Card) (error, bool) {
	err, jsonResp, status := cs.makeRequest(ctx,
		"DELETE",
		fmt.Sprintf("v1/cards/%d", *card.Id),
		"application/x-www-form-urlencoded", "")

	if err != nil {
		return fmt.Errorf("cat not make delete card request: %v", err), true
	}

	if *status != 200 {
		return fmt.Errorf("failed to make delete card request. Http status: %d", *status), true
	}

	jsonbody, err := json.Marshal(jsonResp)
	if err != nil {
		return fmt.Errorf("can not marshal delete card json response: %v", err), true
	}

	d := json.NewDecoder(bytes.NewReader(jsonbody))
	d.DisallowUnknownFields()
	if err := d.Decode(card); err != nil {
		return fmt.Errorf("can not decode delete card json body response: %v", err), true
	}

	return nil, false
}

func (cs *HttpClientCardStore) appendToList (l *[]*Card, data *map[string]interface{}) error {
	jsonbody, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("can not marshal query card list json response: %v", err)
	}

	var card Card
	d := json.NewDecoder(bytes.NewReader(jsonbody))
	d.DisallowUnknownFields()
	if err := d.Decode(&card); err != nil {
		return fmt.Errorf("can not decode query card list json body response: %v", err)
	}
	*l = append(*l, &card)

	return nil
}

func (cs *HttpClientCardStore) Query(ctx interface{}, specification CardSpecification) (error, int, []*Card) {
	var l []*Card
	var c int = 0

	err, jsonResp, _ := cs.makeRequest(ctx, "GET", fmt.Sprintf(
		"v1/cards%s",
		specification.ToQwrStr()),
	"application/x-www-form-urlencoded", "")
	if err != nil {
		return fmt.Errorf("can not make query card request: %v", err), c, l
	}

	if data, ok := (*jsonResp)["data"]; ok {
		switch content := data.(type) {
		case []interface{}:
			// @note: content is list of json nodes
			for _, row := range content {
				if r, ok := row.(map[string]interface{}); ok {
					if err := cs.appendToList(&l, &r); err != nil {
						return fmt.Errorf("can not append to list: %v", err), c, l
					}
				} else {
					return errors.New("card query response list has wrong type"), c, l
				}
			}
		case interface{}:
			// @note: content is json node
			if r, ok := content.(map[string]interface{}); ok {
				if err := cs.appendToList(&l, &r); err != nil {
					return fmt.Errorf("can not append to list: %v", err), c, l
				}
			} else {
				return errors.New("card query response has wrong type"), c, l
			}
		default:
			//@note: content is unknown type
			return errors.New("card query response has wrong type"), c, l
		}
	} else {
		if err := cs.appendToList(&l, jsonResp); err != nil {
			return fmt.Errorf("can not append to list: %v", err), c, l
		}
	}

	return nil, c, l
}

func NewHttpClientCardStore(
	url string,
	client *http.Client,
	logger LoggerFunc,
) CardRepository {
	return &HttpClientCardStore{
		url:    url,
		client: client,
		logger: logger,
	}
}
