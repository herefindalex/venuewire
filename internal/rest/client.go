package rest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const defaultRecvWindow int64 = 5000

type Client struct {
	baseURL    string
	apiKey     string
	apiSecret  string
	recvWindow int64
	httpClient *http.Client
	now        func() time.Time
	requests   atomic.Uint64
	errors     atomic.Uint64
	latencyNS  atomic.Int64
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) { c.httpClient = client }
}

func WithClock(now func() time.Time) Option {
	return func(c *Client) { c.now = now }
}

func WithRecvWindow(window int64) Option {
	return func(c *Client) { c.recvWindow = window }
}

func NewClient(baseURL, apiKey, apiSecret string, options ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		recvWindow: defaultRecvWindow,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		now:        time.Now,
	}
	for _, option := range options {
		option(c)
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	httpClientCopy := *c.httpClient
	httpClientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("Bybit HTTP redirects are disabled")
	}
	c.httpClient = &httpClientCopy
	return c
}

func GenerateOrderLinkID(now time.Time) (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate orderLinkId entropy: %w", err)
	}
	id := "bcx-" + now.UTC().Format("20060102T150405") + "-" + hex.EncodeToString(random)
	if len(id) > 36 {
		return "", errors.New("generated orderLinkId exceeds Bybit limit")
	}
	return id, nil
}

func (c *Client) ServerTime(ctx context.Context) (ServerTime, ResponseMeta, error) {
	var result struct {
		TimeSecond string `json:"timeSecond"`
		TimeNano   string `json:"timeNano"`
	}
	meta, envelopeTime, err := c.do(ctx, http.MethodGet, "/v5/market/time", nil, nil, false, &result)
	if err != nil {
		return ServerTime{}, meta, err
	}
	millis := envelopeTime
	if nanos, parseErr := strconv.ParseInt(result.TimeNano, 10, 64); parseErr == nil && nanos > 0 {
		millis = nanos / int64(time.Millisecond)
	} else if seconds, parseErr := strconv.ParseInt(result.TimeSecond, 10, 64); parseErr == nil {
		millis = seconds * 1000
	}
	return ServerTime{UnixMilli: millis}, meta, nil
}

func (c *Client) Instruments(ctx context.Context, category, symbol string) ([]Instrument, ResponseMeta, error) {
	query := url.Values{"category": {category}}
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	var result struct {
		List []Instrument `json:"list"`
	}
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/market/instruments-info", query, nil, false, &result)
	if err == nil && strings.EqualFold(category, "spot") {
		for i := range result.List {
			if strings.TrimSpace(result.List[i].LotSizeFilter.QtyStep) == "" {
				result.List[i].LotSizeFilter.QtyStep = result.List[i].LotSizeFilter.BasePrecision
			}
		}
	}
	return result.List, meta, err
}

func (c *Client) Tickers(ctx context.Context, category, symbol string) ([]Ticker, ResponseMeta, error) {
	query := url.Values{"category": {category}}
	setIfNotEmpty(query, "symbol", symbol)
	var result struct {
		List []Ticker `json:"list"`
	}
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/market/tickers", query, nil, false, &result)
	return result.List, meta, err
}

func (c *Client) PlaceOrder(ctx context.Context, request PlaceOrderRequest) (OrderAck, ResponseMeta, error) {
	if request.OrderLinkID == "" {
		id, err := GenerateOrderLinkID(c.now())
		if err != nil {
			return OrderAck{}, ResponseMeta{}, err
		}
		request.OrderLinkID = id
	}
	if err := validatePlaceOrder(request); err != nil {
		return OrderAck{}, ResponseMeta{}, err
	}
	var result OrderAck
	meta, _, err := c.do(ctx, http.MethodPost, "/v5/order/create", nil, request, true, &result)
	if err != nil {
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			return OrderAck{}, meta, &UncertainSubmissionError{OrderLinkID: request.OrderLinkID, Cause: err}
		}
	}
	if result.OrderLinkID == "" {
		result.OrderLinkID = request.OrderLinkID
	}
	return result, meta, err
}

func (c *Client) CancelOrder(ctx context.Context, request CancelOrderRequest) (OrderAck, ResponseMeta, error) {
	if request.Category == "" || request.Symbol == "" || (request.OrderID == "" && request.OrderLinkID == "") {
		return OrderAck{}, ResponseMeta{}, errors.New("cancel requires category, symbol, and orderId or orderLinkId")
	}
	var result OrderAck
	meta, _, err := c.do(ctx, http.MethodPost, "/v5/order/cancel", nil, request, true, &result)
	return result, meta, err
}

func (c *Client) AmendOrder(ctx context.Context, request AmendOrderRequest) (OrderAck, ResponseMeta, error) {
	if request.Category == "" || request.Symbol == "" || (request.OrderID == "" && request.OrderLinkID == "") || (request.Qty == "" && request.Price == "") {
		return OrderAck{}, ResponseMeta{}, errors.New("amend requires category, symbol, an order identifier, and qty or price")
	}
	var result OrderAck
	meta, _, err := c.do(ctx, http.MethodPost, "/v5/order/amend", nil, request, true, &result)
	return result, meta, err
}

func validatePlaceOrder(request PlaceOrderRequest) error {
	if request.Category == "" || request.Symbol == "" || request.OrderLinkID == "" {
		return errors.New("place order requires category, symbol, and orderLinkId")
	}
	if len(request.OrderLinkID) > 36 {
		return errors.New("orderLinkId exceeds 36 characters")
	}
	if request.Side != "Buy" && request.Side != "Sell" {
		return errors.New("side must be Buy or Sell")
	}
	if request.OrderType != "Limit" && request.OrderType != "Market" {
		return errors.New("orderType must be Limit or Market")
	}
	if request.OrderType == "Limit" && request.Price == "" {
		return errors.New("Limit order requires price")
	}
	if !positiveDecimal(request.Qty) {
		return errors.New("qty must be a positive decimal")
	}
	if request.Price != "" && !positiveDecimal(request.Price) {
		return errors.New("price must be a positive decimal")
	}
	return nil
}
func positiveDecimal(value string) bool {
	number, ok := new(big.Rat).SetString(value)
	return ok && number.Sign() > 0
}

func (c *Client) Orders(ctx context.Context, category, symbol, orderID, orderLinkID string, openOnly *int) ([]Order, ResponseMeta, error) {
	page, meta, err := c.OrdersPage(ctx, category, symbol, orderID, orderLinkID, openOnly, "")
	return page.List, meta, err
}

func (c *Client) OrdersPage(ctx context.Context, category, symbol, orderID, orderLinkID string, openOnly *int, cursor string) (OrderPage, ResponseMeta, error) {
	return c.orderPage(ctx, "/v5/order/realtime", category, symbol, orderID, orderLinkID, openOnly, cursor)
}

func (c *Client) OrderHistoryPage(ctx context.Context, category, symbol, orderID, orderLinkID, cursor string) (OrderPage, ResponseMeta, error) {
	return c.orderPage(ctx, "/v5/order/history", category, symbol, orderID, orderLinkID, nil, cursor)
}

func (c *Client) orderPage(ctx context.Context, path, category, symbol, orderID, orderLinkID string, openOnly *int, cursor string) (OrderPage, ResponseMeta, error) {
	query := url.Values{"category": {category}}
	setIfNotEmpty(query, "symbol", symbol)
	setIfNotEmpty(query, "orderId", orderID)
	setIfNotEmpty(query, "orderLinkId", orderLinkID)
	if openOnly != nil {
		query.Set("openOnly", strconv.Itoa(*openOnly))
	}
	setIfNotEmpty(query, "cursor", cursor)
	var result struct {
		List           []Order `json:"list"`
		NextPageCursor string  `json:"nextPageCursor"`
	}
	meta, _, err := c.do(ctx, http.MethodGet, path, query, nil, true, &result)
	return OrderPage{List: result.List, NextPageCursor: result.NextPageCursor}, meta, err
}

func (c *Client) Executions(ctx context.Context, category, symbol, orderID, orderLinkID string) ([]Execution, ResponseMeta, error) {
	page, meta, err := c.ExecutionsPage(ctx, category, symbol, orderID, orderLinkID, "")
	return page.List, meta, err
}

func (c *Client) ExecutionsPage(ctx context.Context, category, symbol, orderID, orderLinkID, cursor string) (ExecutionPage, ResponseMeta, error) {
	query := url.Values{"category": {category}}
	setIfNotEmpty(query, "symbol", symbol)
	setIfNotEmpty(query, "orderId", orderID)
	setIfNotEmpty(query, "orderLinkId", orderLinkID)
	setIfNotEmpty(query, "cursor", cursor)
	var result struct {
		List           []Execution `json:"list"`
		NextPageCursor string      `json:"nextPageCursor"`
	}
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/execution/list", query, nil, true, &result)
	return ExecutionPage{List: result.List, NextPageCursor: result.NextPageCursor}, meta, err
}

func (c *Client) Positions(ctx context.Context, category, symbol string) ([]Position, ResponseMeta, error) {
	query := url.Values{"category": {category}}
	setIfNotEmpty(query, "symbol", symbol)
	var result struct {
		List []Position `json:"list"`
	}
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/position/list", query, nil, true, &result)
	return result.List, meta, err
}

func setIfNotEmpty(query url.Values, key, value string) {
	if value != "" {
		query.Set(key, value)
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, private bool, result any) (meta ResponseMeta, envelopeTime int64, err error) {
	started := time.Now()
	c.requests.Add(1)
	defer func() {
		meta.Latency = time.Since(started)
		c.latencyNS.Add(meta.Latency.Nanoseconds())
		if err != nil {
			c.errors.Add(1)
		}
	}()
	queryString := ""
	if query != nil {
		queryString = query.Encode()
	}
	bodyBytes := []byte(nil)
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return ResponseMeta{}, 0, fmt.Errorf("encode request body: %w", err)
		}
	}
	payload := queryString
	if method != http.MethodGet {
		payload = string(bodyBytes)
	}
	endpoint := c.baseURL + path
	if queryString != "" {
		endpoint += "?" + queryString
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return ResponseMeta{}, 0, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if private {
		timestamp := c.now().UTC().UnixMilli()
		req.Header.Set("X-BAPI-API-KEY", c.apiKey)
		req.Header.Set("X-BAPI-TIMESTAMP", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-BAPI-RECV-WINDOW", strconv.FormatInt(c.recvWindow, 10))
		req.Header.Set("X-BAPI-SIGN", Sign(timestamp, c.apiKey, c.recvWindow, payload, c.apiSecret))
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return ResponseMeta{}, 0, fmt.Errorf("send %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	meta = ResponseMeta{RateLimit: parseRateLimit(response.Header), RequestID: response.Header.Get("Traceid")}
	limited := io.LimitReader(response.Body, 4<<20)
	var envelope struct {
		RetCode int64           `json:"retCode"`
		RetMsg  string          `json:"retMsg"`
		Result  json.RawMessage `json:"result"`
		Time    int64           `json:"time"`
	}
	if err := json.NewDecoder(limited).Decode(&envelope); err != nil {
		return meta, 0, fmt.Errorf("decode Bybit response (http=%d): %w", response.StatusCode, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.RetCode != 0 {
		return meta, envelope.Time, &APIError{HTTPStatus: response.StatusCode, Code: envelope.RetCode, Message: envelope.RetMsg, RequestID: meta.RequestID, RateLimit: meta.RateLimit}
	}
	if result != nil && len(envelope.Result) != 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return meta, envelope.Time, fmt.Errorf("decode Bybit result: %w", err)
		}
	}
	return meta, envelope.Time, nil
}

func (c *Client) Stats() ClientStats {
	return ClientStats{Requests: c.requests.Load(), Errors: c.errors.Load(), TotalLatency: time.Duration(c.latencyNS.Load())}
}

func parseRateLimit(header http.Header) RateLimit {
	parse := func(name string) int64 {
		value, _ := strconv.ParseInt(header.Get(name), 10, 64)
		return value
	}
	resetMillis := parse("X-Bapi-Limit-Reset-Timestamp")
	return RateLimit{
		Limit:     parse("X-Bapi-Limit"),
		Remaining: parse("X-Bapi-Limit-Status"),
		ResetAt:   time.UnixMilli(resetMillis).UTC(),
	}
}
