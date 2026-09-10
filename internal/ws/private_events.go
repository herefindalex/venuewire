package ws

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"venuewire/internal/domain"
)

type PrivateEventKind string

const (
	PrivateOrder     PrivateEventKind = "order"
	PrivateExecution PrivateEventKind = "execution"
	PrivatePosition  PrivateEventKind = "position"
	PrivateWallet    PrivateEventKind = "wallet"
)

type PrivateEvent struct {
	Kind         PrivateEventKind
	EventID      string
	Topic        string
	ExchangeTime time.Time
	ReceivedAt   time.Time
	Order        *domain.Order
	Execution    *domain.Execution
	Position     *domain.Position
}

func DecodePrivate(payload []byte, receivedAt time.Time) ([]PrivateEvent, error) {
	var message struct {
		ID           string          `json:"id"`
		Topic        string          `json:"topic"`
		CreationTime int64           `json:"creationTime"`
		Data         json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &message); err != nil {
		return nil, fmt.Errorf("decode private WebSocket envelope: %w", err)
	}
	if message.Topic == "" {
		return nil, nil
	}
	exchangeTime := time.UnixMilli(message.CreationTime).UTC()
	switch {
	case hasPrefix(message.Topic, "wallet"):
		var rows []json.RawMessage
		if err := json.Unmarshal(message.Data, &rows); err != nil {
			return nil, fmt.Errorf("decode private wallet: %w", err)
		}
		events := make([]PrivateEvent, 0, len(rows))
		for i := range rows {
			events = append(events, PrivateEvent{Kind: PrivateWallet, EventID: fmt.Sprintf("%s:%d", message.ID, i), Topic: message.Topic, ExchangeTime: exchangeTime, ReceivedAt: receivedAt})
		}
		return events, nil
	case hasPrefix(message.Topic, "order"):
		var rows []struct {
			Category    string `json:"category"`
			Symbol      string `json:"symbol"`
			OrderID     string `json:"orderId"`
			OrderLinkID string `json:"orderLinkId"`
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
		if err := json.Unmarshal(message.Data, &rows); err != nil {
			return nil, fmt.Errorf("decode private order: %w", err)
		}
		events := make([]PrivateEvent, 0, len(rows))
		for i, row := range rows {
			created := millisString(row.CreatedTime)
			updated := millisString(row.UpdatedTime)
			order := &domain.Order{Exchange: "bybit", Category: row.Category, Symbol: row.Symbol, OrderID: row.OrderID, OrderLinkID: row.OrderLinkID, Side: domain.Side(row.Side), Type: domain.OrderType(row.OrderType), Price: row.Price, Qty: row.Qty, CumFilledQty: row.CumExecQty, AvgFillPrice: row.AvgPrice, Status: domain.NormalizeBybitOrderStatus(row.OrderStatus), RawStatus: row.OrderStatus, CreatedAt: created, UpdatedAt: updated}
			events = append(events, PrivateEvent{Kind: PrivateOrder, EventID: fmt.Sprintf("%s:%d", message.ID, i), Topic: message.Topic, ExchangeTime: exchangeTime, ReceivedAt: receivedAt, Order: order})
		}
		return events, nil
	case hasPrefix(message.Topic, "execution"):
		var rows []struct {
			Category    string `json:"category"`
			Symbol      string `json:"symbol"`
			ExecID      string `json:"execId"`
			OrderID     string `json:"orderId"`
			OrderLinkID string `json:"orderLinkId"`
			Side        string `json:"side"`
			ExecPrice   string `json:"execPrice"`
			ExecQty     string `json:"execQty"`
			ExecTime    string `json:"execTime"`
		}
		if err := json.Unmarshal(message.Data, &rows); err != nil {
			return nil, fmt.Errorf("decode private execution: %w", err)
		}
		events := make([]PrivateEvent, 0, len(rows))
		for _, row := range rows {
			timestamp := millisString(row.ExecTime)
			execution := &domain.Execution{Exchange: "bybit", Category: row.Category, Symbol: row.Symbol, ExecutionID: row.ExecID, OrderID: row.OrderID, OrderLinkID: row.OrderLinkID, Side: domain.Side(row.Side), Price: row.ExecPrice, Qty: row.ExecQty, ExchangeTime: timestamp, ReceivedAt: receivedAt}
			events = append(events, PrivateEvent{Kind: PrivateExecution, EventID: row.ExecID, Topic: message.Topic, ExchangeTime: timestamp, ReceivedAt: receivedAt, Execution: execution})
		}
		return events, nil
	case hasPrefix(message.Topic, "position"):
		var rows []struct {
			Category   string `json:"category"`
			Symbol     string `json:"symbol"`
			Side       string `json:"side"`
			Size       string `json:"size"`
			EntryPrice string `json:"entryPrice"`
			MarkPrice  string `json:"markPrice"`
			UnrealPnL  string `json:"unrealisedPnl"`
		}
		if err := json.Unmarshal(message.Data, &rows); err != nil {
			return nil, fmt.Errorf("decode private position: %w", err)
		}
		events := make([]PrivateEvent, 0, len(rows))
		for i, row := range rows {
			position := &domain.Position{Exchange: "bybit", Category: row.Category, Symbol: row.Symbol, Side: domain.Side(row.Side), Size: row.Size, EntryPrice: row.EntryPrice, MarkPrice: row.MarkPrice, UnrealizedPnL: row.UnrealPnL, ExchangeTime: exchangeTime, ReceivedAt: receivedAt}
			events = append(events, PrivateEvent{Kind: PrivatePosition, EventID: fmt.Sprintf("%s:%d", message.ID, i), Topic: message.Topic, ExchangeTime: exchangeTime, ReceivedAt: receivedAt, Position: position})
		}
		return events, nil
	default:
		return nil, fmt.Errorf("unsupported private topic %q", message.Topic)
	}
}

func millisString(value string) time.Time {
	millis, err := strconv.ParseInt(value, 10, 64)
	if err != nil || millis == 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis).UTC()
}
