package deribit

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"time"
)

type TickSizeStep struct {
	AbovePrice json.Number `json:"above_price"`
	TickSize   json.Number `json:"tick_size"`
}

type Instrument struct {
	InstrumentName      string         `json:"instrument_name"`
	Kind                string         `json:"kind"`
	BaseCurrency        string         `json:"base_currency"`
	CounterCurrency     string         `json:"counter_currency"`
	SettlementCurrency  string         `json:"settlement_currency"`
	QuoteCurrency       string         `json:"quote_currency"`
	TickSize            json.Number    `json:"tick_size"`
	TickSizeSteps       []TickSizeStep `json:"tick_size_steps,omitempty"`
	AmountStep          json.Number    `json:"amount_step"`
	MinTradeAmount      json.Number    `json:"min_trade_amount"`
	MakerCommission     json.Number    `json:"maker_commission"`
	TakerCommission     json.Number    `json:"taker_commission"`
	ContractSize        json.Number    `json:"contract_size"`
	IsActive            bool           `json:"is_active"`
	InstrumentID        int64          `json:"instrument_id"`
	ExpirationTimestamp int64          `json:"expiration_timestamp"`
}

func (i Instrument) EffectiveTickSize(price string) (json.Number, error) {
	priceValue, ok := new(big.Rat).SetString(price)
	if !ok || priceValue.Sign() <= 0 {
		return "", errors.New("price must be a positive decimal")
	}
	if tick, ok := new(big.Rat).SetString(i.TickSize.String()); !ok || tick.Sign() <= 0 {
		return "", errors.New("instrument tick_size must be a positive decimal")
	}

	selected := i.TickSize
	var selectedThreshold *big.Rat
	for _, step := range i.TickSizeSteps {
		threshold, thresholdOK := new(big.Rat).SetString(step.AbovePrice.String())
		stepTick, tickOK := new(big.Rat).SetString(step.TickSize.String())
		if !thresholdOK || threshold.Sign() < 0 || !tickOK || stepTick.Sign() <= 0 {
			return "", errors.New("instrument tick_size_steps contains an invalid decimal")
		}
		if priceValue.Cmp(threshold) <= 0 {
			continue
		}
		if selectedThreshold == nil || threshold.Cmp(selectedThreshold) > 0 {
			selectedThreshold = threshold
			selected = step.TickSize
		}
	}
	return selected, nil
}

type CancelOnDisconnect struct {
	Scope   string `json:"scope"`
	Enabled bool   `json:"enabled"`
}

type AccountSummary struct {
	Currency                 string      `json:"currency"`
	Balance                  json.Number `json:"balance"`
	Equity                   json.Number `json:"equity"`
	AvailableFunds           json.Number `json:"available_funds"`
	AvailableWithdrawalFunds json.Number `json:"available_withdrawal_funds"`
	InitialMargin            json.Number `json:"initial_margin"`
	MaintenanceMargin        json.Number `json:"maintenance_margin"`
	MarginBalance            json.Number `json:"margin_balance"`
}

type Position struct {
	InstrumentName     string      `json:"instrument_name"`
	Kind               string      `json:"kind"`
	Direction          string      `json:"direction"`
	Size               json.Number `json:"size"`
	SizeCurrency       json.Number `json:"size_currency"`
	AveragePrice       json.Number `json:"average_price"`
	MarkPrice          json.Number `json:"mark_price"`
	FloatingProfitLoss json.Number `json:"floating_profit_loss"`
	RealizedProfitLoss json.Number `json:"realized_profit_loss"`
}

type Currency struct {
	Currency string `json:"currency"`
	CoinType string `json:"coin_type"`
}

func (c *Client) ServerTime(ctx context.Context) (int64, error) {
	var result int64
	err := c.Public(ctx, "public/get_time", nil, &result)
	return result, err
}

func (c *Client) Currencies(ctx context.Context) ([]Currency, error) {
	var result []Currency
	err := c.Public(ctx, "public/get_currencies", nil, &result)
	return result, err
}

func (c *Client) Instruments(ctx context.Context, currency, kind string, expired bool) ([]Instrument, error) {
	var result []Instrument
	err := c.Public(ctx, "public/get_instruments", map[string]any{"currency": currency, "kind": kind, "expired": expired}, &result)
	return result, err
}

func (c *Client) Instrument(ctx context.Context, name string) (Instrument, error) {
	c.metadataMu.Lock()
	cached, ok := c.metadata[name]
	now := c.now()
	if ok && now.Before(cached.ExpiresAt) {
		c.metadataMu.Unlock()
		return cached.Value, nil
	}
	c.metadataMu.Unlock()
	var result Instrument
	err := c.Public(ctx, "public/get_instrument", map[string]string{"instrument_name": name}, &result)
	if err == nil {
		c.metadataMu.Lock()
		if c.metadata == nil {
			c.metadata = map[string]cachedInstrument{}
		}
		c.metadata[name] = cachedInstrument{Value: result, ExpiresAt: now.Add(5 * time.Minute)}
		c.metadataMu.Unlock()
	}
	return result, err
}

func (c *Client) AccountSummary(ctx context.Context, currency string) (AccountSummary, error) {
	var result AccountSummary
	err := c.PrivateRead(ctx, "private/get_account_summary", map[string]any{"currency": currency, "extended": true}, &result)
	return result, err
}

func (c *Client) AccountSummaries(ctx context.Context) ([]AccountSummary, error) {
	var raw json.RawMessage
	if err := c.PrivateRead(ctx, "private/get_account_summaries", map[string]any{"extended": true}, &raw); err != nil {
		return nil, err
	}
	var direct []AccountSummary
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		Summaries []AccountSummary `json:"summaries"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Summaries, nil
}

func (c *Client) Positions(ctx context.Context, currency, kind string) ([]Position, error) {
	params := map[string]string{"currency": currency}
	if kind != "" {
		params["kind"] = kind
	}
	var result []Position
	err := c.PrivateRead(ctx, "private/get_positions", params, &result)
	return result, err
}

func (c *Client) CancelOnDisconnect(ctx context.Context, scope string) (CancelOnDisconnect, error) {
	var result CancelOnDisconnect
	err := c.PrivateRead(ctx, "private/get_cancel_on_disconnect", map[string]string{"scope": scope}, &result)
	return result, err
}
