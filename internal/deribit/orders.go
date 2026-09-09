package deribit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type OutcomeUnknownError struct {
	Method string
	Cause  error
}

func (e *OutcomeUnknownError) Error() string {
	return fmt.Sprintf("%s outcome unknown: %v", e.Method, e.Cause)
}
func (e *OutcomeUnknownError) Unwrap() error { return e.Cause }

type Order struct {
	OrderID             string      `json:"order_id"`
	Label               string      `json:"label"`
	InstrumentName      string      `json:"instrument_name"`
	Direction           string      `json:"direction"`
	OrderType           string      `json:"order_type"`
	OrderState          string      `json:"order_state"`
	Amount              json.Number `json:"amount"`
	FilledAmount        json.Number `json:"filled_amount"`
	Price               json.Number `json:"price"`
	AveragePrice        json.Number `json:"average_price"`
	Commission          json.Number `json:"commission"`
	CreationTimestamp   int64       `json:"creation_timestamp"`
	LastUpdateTimestamp int64       `json:"last_update_timestamp"`
	ReduceOnly          bool        `json:"reduce_only"`
}

type Trade struct {
	TradeID        string      `json:"trade_id"`
	OrderID        string      `json:"order_id"`
	InstrumentName string      `json:"instrument_name"`
	Direction      string      `json:"direction"`
	Amount         json.Number `json:"amount"`
	Price          json.Number `json:"price"`
	Fee            json.Number `json:"fee"`
	FeeCurrency    string      `json:"fee_currency"`
	Timestamp      int64       `json:"timestamp"`
}

type OrderResult struct {
	Order  Order   `json:"order"`
	Trades []Trade `json:"trades"`
}

type Ticker struct {
	InstrumentName string      `json:"instrument_name"`
	MarkPrice      json.Number `json:"mark_price"`
	BestBidPrice   json.Number `json:"best_bid_price"`
	BestAskPrice   json.Number `json:"best_ask_price"`
}

type PlaceParams struct {
	InstrumentName string `json:"instrument_name"`
	Amount         string `json:"amount"`
	Type           string `json:"type"`
	Label          string `json:"label"`
	Price          string `json:"price,omitempty"`
	TimeInForce    string `json:"time_in_force,omitempty"`
	PostOnly       bool   `json:"post_only,omitempty"`
	ReduceOnly     bool   `json:"reduce_only,omitempty"`
}

func (c *Client) PrivateWrite(ctx context.Context, method string, params any, result any) error {
	allowed := map[string]bool{"private/buy": true, "private/sell": true, "private/edit": true, "private/edit_by_label": true, "private/cancel": true, "private/cancel_by_label": true}
	if !allowed[method] {
		return errors.New("unsupported private write method")
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return err
	}
	err = c.call(ctx, method, params, token, result)
	if err == nil {
		return nil
	}
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return err
	}
	return &OutcomeUnknownError{Method: method, Cause: err}
}

func (c *Client) Place(ctx context.Context, side string, params PlaceParams) (OrderResult, error) {
	side = strings.ToLower(side)
	if side != "buy" && side != "sell" {
		return OrderResult{}, errors.New("side must be buy or sell")
	}
	var result OrderResult
	err := c.PrivateWrite(ctx, "private/"+side, params, &result)
	return result, err
}

func (c *Client) Edit(ctx context.Context, orderID, amount, price string) (OrderResult, error) {
	params := map[string]any{"order_id": orderID, "amount": amount}
	if price != "" {
		params["price"] = price
	}
	var result OrderResult
	err := c.PrivateWrite(ctx, "private/edit", params, &result)
	return result, err
}

func (c *Client) Cancel(ctx context.Context, orderID string) (Order, error) {
	var result Order
	err := c.PrivateWrite(ctx, "private/cancel", map[string]string{"order_id": orderID}, &result)
	return result, err
}

func (c *Client) Ticker(ctx context.Context, instrument string) (Ticker, error) {
	var result Ticker
	err := c.Public(ctx, "public/ticker", map[string]string{"instrument_name": instrument}, &result)
	return result, err
}

func (c *Client) OrderState(ctx context.Context, orderID string) (Order, error) {
	var result Order
	err := c.PrivateRead(ctx, "private/get_order_state", map[string]string{"order_id": orderID}, &result)
	return result, err
}

func (c *Client) OpenOrders(ctx context.Context, instrument string) ([]Order, error) {
	var result []Order
	err := c.PrivateRead(ctx, "private/get_open_orders_by_instrument", map[string]string{"instrument_name": instrument, "type": "all"}, &result)
	return result, err
}

func (c *Client) OpenFutureOrdersByCurrency(ctx context.Context, currency string) ([]Order, error) {
	var result []Order
	err := c.PrivateRead(ctx, "private/get_open_orders_by_currency", map[string]string{"currency": currency, "kind": "future", "type": "all"}, &result)
	return result, err
}

func (c *Client) TradesByOrder(ctx context.Context, orderID string) ([]Trade, error) {
	var raw json.RawMessage
	if err := c.PrivateRead(ctx, "private/get_user_trades_by_order", map[string]any{"order_id": orderID, "sorting": "asc"}, &raw); err != nil {
		return nil, err
	}
	var direct []Trade
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var result struct {
		Trades  []Trade `json:"trades"`
		HasMore bool    `json:"has_more"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result.Trades, nil
}
