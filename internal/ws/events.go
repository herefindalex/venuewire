package ws

import (
	"encoding/json"
	"fmt"
	"time"
)

type EventKind string

const (
	EventTrade     EventKind = "trade"
	EventOrderBook EventKind = "orderbook"
)

type Event struct {
	Kind         EventKind
	Topic        string
	Symbol       string
	ExchangeTime time.Time
	ReceivedAt   time.Time
	Lag          time.Duration
	Trade        *Trade
	OrderBook    *OrderBook
}

type Trade struct {
	ID         string
	Side       string
	Price      string
	Size       string
	TickDir    string
	BlockTrade bool
}

type PriceLevel struct {
	Price string
	Size  string
}

type OrderBook struct {
	UpdateType string
	UpdateID   int64
	Sequence   int64
	Bids       []PriceLevel
	Asks       []PriceLevel
}

type envelope struct {
	Topic string          `json:"topic"`
	Type  string          `json:"type"`
	TS    int64           `json:"ts"`
	CTS   int64           `json:"cts"`
	Data  json.RawMessage `json:"data"`
}

func DecodePublic(payload []byte, receivedAt time.Time) ([]Event, error) {
	var message envelope
	if err := json.Unmarshal(payload, &message); err != nil {
		return nil, fmt.Errorf("decode WebSocket envelope: %w", err)
	}
	if message.Topic == "" {
		return nil, nil // subscription acknowledgements and pong responses
	}
	exchangeMillis := message.TS
	if message.CTS != 0 {
		exchangeMillis = message.CTS
	}
	exchangeTime := time.UnixMilli(exchangeMillis).UTC()
	lag := receivedAt.Sub(exchangeTime)
	switch {
	case hasPrefix(message.Topic, "publicTrade."):
		var rows []struct {
			Timestamp  int64  `json:"T"`
			Symbol     string `json:"s"`
			Side       string `json:"S"`
			Size       string `json:"v"`
			Price      string `json:"p"`
			TickDir    string `json:"L"`
			ID         string `json:"i"`
			BlockTrade bool   `json:"BT"`
		}
		if err := json.Unmarshal(message.Data, &rows); err != nil {
			return nil, fmt.Errorf("decode public trade: %w", err)
		}
		events := make([]Event, 0, len(rows))
		for _, row := range rows {
			rowTime := exchangeTime
			if row.Timestamp != 0 {
				rowTime = time.UnixMilli(row.Timestamp).UTC()
			}
			events = append(events, Event{Kind: EventTrade, Topic: message.Topic, Symbol: row.Symbol, ExchangeTime: rowTime, ReceivedAt: receivedAt, Lag: receivedAt.Sub(rowTime), Trade: &Trade{ID: row.ID, Side: row.Side, Price: row.Price, Size: row.Size, TickDir: row.TickDir, BlockTrade: row.BlockTrade}})
		}
		return events, nil
	case hasPrefix(message.Topic, "orderbook."):
		var row struct {
			Symbol   string     `json:"s"`
			Bids     [][]string `json:"b"`
			Asks     [][]string `json:"a"`
			UpdateID int64      `json:"u"`
			Sequence int64      `json:"seq"`
		}
		if err := json.Unmarshal(message.Data, &row); err != nil {
			return nil, fmt.Errorf("decode public orderbook: %w", err)
		}
		book := &OrderBook{UpdateType: message.Type, UpdateID: row.UpdateID, Sequence: row.Sequence, Bids: levels(row.Bids), Asks: levels(row.Asks)}
		return []Event{{Kind: EventOrderBook, Topic: message.Topic, Symbol: row.Symbol, ExchangeTime: exchangeTime, ReceivedAt: receivedAt, Lag: lag, OrderBook: book}}, nil
	default:
		return nil, fmt.Errorf("unsupported public topic %q", message.Topic)
	}
}

func levels(rows [][]string) []PriceLevel {
	result := make([]PriceLevel, 0, len(rows))
	for _, row := range rows {
		if len(row) >= 2 {
			result = append(result, PriceLevel{Price: row[0], Size: row[1]})
		}
	}
	return result
}

func hasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
