package rest

import (
	"fmt"
	"strconv"
	"time"
)

type RateLimit struct {
	Limit     int64
	Remaining int64
	ResetAt   time.Time
}

type ResponseMeta struct {
	RateLimit RateLimit
	RequestID string
	Latency   time.Duration
}

type ClientStats struct {
	Requests     uint64
	Errors       uint64
	TotalLatency time.Duration
}

type APIError struct {
	HTTPStatus int
	Code       int64
	Message    string
	RequestID  string
	RateLimit  RateLimit
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Bybit API error: http=%d code=%d message=%q request_id=%q", e.HTTPStatus, e.Code, e.Message, e.RequestID)
}

type UncertainSubmissionError struct {
	OrderLinkID string
	Cause       error
}

func (e *UncertainSubmissionError) Error() string {
	return fmt.Sprintf("order submission outcome is uncertain for orderLinkId %q; reconcile before retrying: %v", e.OrderLinkID, e.Cause)
}

func (e *UncertainSubmissionError) Unwrap() error { return e.Cause }

type ServerTime struct {
	UnixMilli int64
}

type Instrument struct {
	Symbol       string `json:"symbol"`
	ContractType string `json:"contractType"`
	Status       string `json:"status"`
	BaseCoin     string `json:"baseCoin"`
	QuoteCoin    string `json:"quoteCoin"`
	SettleCoin   string `json:"settleCoin"`
	PriceScale   string `json:"priceScale"`
	PriceFilter  struct {
		MinPrice string `json:"minPrice"`
		MaxPrice string `json:"maxPrice"`
		TickSize string `json:"tickSize"`
	} `json:"priceFilter"`
	LotSizeFilter struct {
		BasePrecision string `json:"basePrecision"`
		MinOrderQty   string `json:"minOrderQty"`
		MaxOrderQty   string `json:"maxOrderQty"`
		QtyStep       string `json:"qtyStep"`
		MinNotional   string `json:"minNotionalValue"`
		MinOrderAmt   string `json:"minOrderAmt"`
		MaxOrderAmt   string `json:"maxOrderAmt"`
	} `json:"lotSizeFilter"`
}

type Ticker struct {
	Symbol    string `json:"symbol"`
	LastPrice string `json:"lastPrice"`
	Bid1Price string `json:"bid1Price"`
	Ask1Price string `json:"ask1Price"`
}

type PlaceOrderRequest struct {
	Category    string `json:"category"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	OrderType   string `json:"orderType"`
	Qty         string `json:"qty"`
	Price       string `json:"price,omitempty"`
	TimeInForce string `json:"timeInForce,omitempty"`
	OrderLinkID string `json:"orderLinkId"`
	IsLeverage  *int   `json:"isLeverage,omitempty"`
	ReduceOnly  bool   `json:"reduceOnly,omitempty"`
}

type CancelOrderRequest struct {
	Category    string `json:"category"`
	Symbol      string `json:"symbol"`
	OrderID     string `json:"orderId,omitempty"`
	OrderLinkID string `json:"orderLinkId,omitempty"`
}

type AmendOrderRequest struct {
	Category    string `json:"category"`
	Symbol      string `json:"symbol"`
	OrderID     string `json:"orderId,omitempty"`
	OrderLinkID string `json:"orderLinkId,omitempty"`
	Qty         string `json:"qty,omitempty"`
	Price       string `json:"price,omitempty"`
}

type OrderAck struct {
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
}

type Order struct {
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	OrderType   string `json:"orderType"`
	Price       string `json:"price"`
	Qty         string `json:"qty"`
	CumExecQty  string `json:"cumExecQty"`
	AvgPrice    string `json:"avgPrice"`
	OrderStatus string `json:"orderStatus"`
	CreatedTime string `json:"createdTime"`
	UpdatedTime string `json:"updatedTime"`
}
type OrderPage struct {
	List           []Order
	NextPageCursor string
}

type Execution struct {
	ExecID      string `json:"execId"`
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	ExecPrice   string `json:"execPrice"`
	ExecQty     string `json:"execQty"`
	ExecFee     string `json:"execFee"`
	FeeRate     string `json:"feeRate"`
	FeeCurrency string `json:"feeCurrency"`
	ExecTime    string `json:"execTime"`
}
type ExecutionPage struct {
	List           []Execution
	NextPageCursor string
}

type Position struct {
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	Size      string `json:"size"`
	AvgPrice  string `json:"avgPrice"`
	MarkPrice string `json:"markPrice"`
	UnrealPnL string `json:"unrealisedPnl"`
}

func parseMillis(value string) time.Time {
	millis, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(millis).UTC()
}
