package domain

import "time"

type Side string

const (
	SideBuy  Side = "Buy"
	SideSell Side = "Sell"
)

func NormalizeBybitOrderStatus(raw string) OrderStatus {
	switch raw {
	case "New", "Untriggered", "Triggered", "Active":
		return OrderStatusNew
	case "PartiallyFilled":
		return OrderStatusPartiallyFilled
	case "Filled":
		return OrderStatusFilled
	case "Cancelled", "PartiallyFilledCanceled", "Deactivated":
		return OrderStatusCancelled
	case "Rejected":
		return OrderStatusRejected
	default:
		return OrderStatusUnknown
	}
}

type OrderType string

const (
	OrderTypeLimit  OrderType = "Limit"
	OrderTypeMarket OrderType = "Market"
)

type OrderStatus string

const (
	OrderStatusUnknown         OrderStatus = "Unknown"
	OrderStatusPendingSubmit   OrderStatus = "PendingSubmit"
	OrderStatusNew             OrderStatus = "New"
	OrderStatusPartiallyFilled OrderStatus = "PartiallyFilled"
	OrderStatusFilled          OrderStatus = "Filled"
	OrderStatusPendingCancel   OrderStatus = "PendingCancel"
	OrderStatusCancelled       OrderStatus = "Cancelled"
	OrderStatusRejected        OrderStatus = "Rejected"
	OrderStatusRejectedCancel  OrderStatus = "RejectedCancel"
)

// Decimal quantities remain strings so protocol boundaries never silently
// lose precision through binary floating-point conversion.
type Order struct {
	Exchange     string      `json:"exchange"`
	Environment  string      `json:"environment,omitempty"`
	AccountAlias string      `json:"accountAlias,omitempty"`
	Category     string      `json:"category"`
	Symbol       string      `json:"symbol"`
	OrderID      string      `json:"orderId"`
	OrderLinkID  string      `json:"orderLinkId"`
	Side         Side        `json:"side"`
	Type         OrderType   `json:"type"`
	Price        string      `json:"price"`
	Qty          string      `json:"qty"`
	CumFilledQty string      `json:"cumFilledQty"`
	AvgFillPrice string      `json:"avgFillPrice"`
	Status       OrderStatus `json:"status"`
	RawStatus    string      `json:"rawStatus"`
	IntentID     string      `json:"intentId,omitempty"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

type Execution struct {
	Exchange     string    `json:"exchange"`
	Environment  string    `json:"environment,omitempty"`
	AccountAlias string    `json:"accountAlias,omitempty"`
	Category     string    `json:"category"`
	Symbol       string    `json:"symbol"`
	ExecutionID  string    `json:"executionId"`
	OrderID      string    `json:"orderId"`
	OrderLinkID  string    `json:"orderLinkId"`
	Side         Side      `json:"side"`
	Price        string    `json:"price"`
	Qty          string    `json:"qty"`
	Fee          string    `json:"fee,omitempty"`
	FeeCurrency  string    `json:"feeCurrency,omitempty"`
	ExchangeTime time.Time `json:"exchangeTime"`
	ReceivedAt   time.Time `json:"receivedAt"`
}

type Position struct {
	Exchange      string    `json:"exchange"`
	Environment   string    `json:"environment,omitempty"`
	AccountAlias  string    `json:"accountAlias,omitempty"`
	Category      string    `json:"category"`
	Symbol        string    `json:"symbol"`
	Side          Side      `json:"side"`
	Size          string    `json:"size"`
	EntryPrice    string    `json:"entryPrice"`
	MarkPrice     string    `json:"markPrice"`
	UnrealizedPnL string    `json:"unrealizedPnl"`
	ExchangeTime  time.Time `json:"exchangeTime"`
	ReceivedAt    time.Time `json:"receivedAt"`
}
