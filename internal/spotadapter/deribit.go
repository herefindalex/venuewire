package spotadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/intent"
	"github.com/herefindalex/venuewire/internal/quicktrade"
)

type DeribitClient interface {
	Instrument(context.Context, string) (deribit.Instrument, error)
	SpotOrderBook(context.Context, string, int) (deribit.SpotOrderBook, error)
	Place(context.Context, string, deribit.PlaceParams) (deribit.OrderResult, error)
}

type Deribit struct {
	Client      DeribitClient
	Accounts    CapacityReader
	MetadataTTL time.Duration
	Now         func() time.Time

	mu    sync.Mutex
	rules map[string]cachedRules
}

func (a *Deribit) Market(ctx context.Context, route quicktrade.Route) (quicktrade.MarketSnapshot, error) {
	if a.Client == nil {
		return quicktrade.MarketSnapshot{}, errors.New("Deribit Spot client is unavailable")
	}
	rules, err := a.instrumentRules(ctx, route)
	if err != nil {
		return quicktrade.MarketSnapshot{}, err
	}
	book, err := a.Client.SpotOrderBook(ctx, route.Instrument, 50)
	if err != nil {
		return quicktrade.MarketSnapshot{}, err
	}
	if book.InstrumentName != route.Instrument || book.TimestampMS <= 0 {
		return quicktrade.MarketSnapshot{}, errors.New("Deribit Spot order book scope or timestamp is invalid")
	}
	bids, err := deribitLevels(book.Bids)
	if err != nil {
		return quicktrade.MarketSnapshot{}, fmt.Errorf("Deribit Spot bids: %w", err)
	}
	asks, err := deribitLevels(book.Asks)
	if err != nil {
		return quicktrade.MarketSnapshot{}, fmt.Errorf("Deribit Spot asks: %w", err)
	}
	return quicktrade.MarketSnapshot{Rules: rules, Bids: bids, Asks: asks, ObservedAt: time.UnixMilli(book.TimestampMS).UTC()}, nil
}

func (a *Deribit) Available(_ context.Context, _ quicktrade.Route, asset string) (quicktrade.Capacity, error) {
	if a.Accounts == nil {
		return quicktrade.Capacity{}, errors.New("Deribit account capacity is unavailable")
	}
	available, accountRevision, observedAt, ok := a.Accounts.Capacity(domain.VenueDeribit, strings.ToUpper(strings.TrimSpace(asset)))
	if !ok {
		return quicktrade.Capacity{}, errors.New("Deribit conservative Spot capacity is stale or unknown")
	}
	return quicktrade.Capacity{Available: available, AccountRevision: accountRevision, ObservedAt: observedAt}, nil
}

func (a *Deribit) Fee(ctx context.Context, route quicktrade.Route) (quicktrade.FeePolicy, error) {
	rules, err := a.instrumentRules(ctx, route)
	if err != nil {
		return quicktrade.FeePolicy{}, err
	}
	// Deribit Spot instrument metadata does not expose a separate fee_currency
	// before execution. Its API-sourced quote currency determines the estimate;
	// reconciliation replaces that estimate with each execution's fee_currency.
	chargeAsset := ""
	switch strings.ToUpper(strings.TrimSpace(rules.QuoteAsset)) {
	case route.FromAsset:
		chargeAsset = "from"
	case route.ToAsset:
		chargeAsset = "to"
	default:
		return quicktrade.FeePolicy{}, errors.New("Deribit Spot fee currency is outside the selected route")
	}
	return quicktrade.FeePolicy{Rate: a.cachedInstrumentFee(route.Instrument), ChargeAsset: chargeAsset, Source: "Deribit Spot instrument taker commission"}, nil
}

func (a *Deribit) Submit(ctx context.Context, trade intent.QuickTrade) (quicktrade.Submission, error) {
	if a.Client == nil {
		return quicktrade.Submission{}, errors.New("Deribit Spot client is unavailable")
	}
	result, err := a.Client.Place(ctx, strings.ToLower(trade.Quote.Side), deribit.PlaceParams{
		InstrumentName: trade.Quote.Instrument,
		Amount:         json.Number(trade.Quote.BaseQty),
		Type:           "limit",
		Price:          json.Number(trade.Quote.LimitPrice),
		Label:          trade.ClientOrderID,
		TimeInForce:    "immediate_or_cancel",
	})
	if err != nil {
		var rejected *deribit.RPCError
		if errors.As(err, &rejected) {
			return quicktrade.Submission{}, &quicktrade.RejectedError{PublicMessage: "Deribit rejected the Spot order.", Err: err}
		}
		return quicktrade.Submission{}, err
	}
	if strings.EqualFold(result.Order.OrderState, "rejected") {
		return quicktrade.Submission{}, &quicktrade.RejectedError{PublicMessage: "Deribit rejected the Spot order."}
	}
	if strings.TrimSpace(result.Order.OrderID) == "" {
		return quicktrade.Submission{}, errors.New("Deribit acknowledged the request without an order ID")
	}
	return quicktrade.Submission{VenueOrderID: result.Order.OrderID, RawVenueStatus: result.Order.OrderState, Accepted: true}, nil
}

func (a *Deribit) instrumentRules(ctx context.Context, route quicktrade.Route) (quicktrade.InstrumentRules, error) {
	now := a.now()
	ttl := a.MetadataTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	a.mu.Lock()
	if cached, ok := a.rules[route.Instrument]; ok && now.Before(cached.expiresAt) {
		a.mu.Unlock()
		return cached.value, nil
	}
	a.mu.Unlock()

	instrument, err := a.Client.Instrument(ctx, route.Instrument)
	if err != nil {
		return quicktrade.InstrumentRules{}, err
	}
	if instrument.InstrumentName != route.Instrument || instrument.Kind != "spot" || !instrument.IsActive {
		return quicktrade.InstrumentRules{}, errors.New("Deribit Spot instrument is not active")
	}
	if len(instrument.TickSizeSteps) != 0 {
		return quicktrade.InstrumentRules{}, errors.New("Deribit segmented Spot tick sizes are not supported by Quick Trade")
	}
	tick, step, minimum := instrument.TickSize.String(), instrument.AmountStep.String(), instrument.MinTradeAmount.String()
	feeRate := instrument.TakerCommission.String()
	if _, ok := positiveDecimal(tick); !ok {
		return quicktrade.InstrumentRules{}, errors.New("Deribit Spot tick size is invalid")
	}
	if _, ok := positiveDecimal(step); !ok {
		return quicktrade.InstrumentRules{}, errors.New("Deribit Spot amount step is invalid")
	}
	if _, ok := positiveDecimal(minimum); !ok {
		return quicktrade.InstrumentRules{}, errors.New("Deribit Spot minimum amount is invalid")
	}
	if _, ok := nonNegativeDecimal(feeRate); !ok {
		return quicktrade.InstrumentRules{}, errors.New("Deribit Spot taker commission is invalid")
	}
	rules := quicktrade.InstrumentRules{
		Instrument: instrument.InstrumentName, BaseAsset: instrument.BaseCurrency, QuoteAsset: instrument.QuoteCurrency,
		TickSize: tick, QuantityStep: step, MinimumQuantity: minimum,
	}
	rules.MetadataRevision = revision(rules.Instrument, rules.BaseAsset, rules.QuoteAsset, rules.TickSize, rules.QuantityStep, rules.MinimumQuantity, feeRate)
	a.mu.Lock()
	if a.rules == nil {
		a.rules = make(map[string]cachedRules)
	}
	a.rules[route.Instrument] = cachedRules{value: rules, expiresAt: now.Add(ttl), feeRate: feeRate}
	a.mu.Unlock()
	return rules, nil
}

func (a *Deribit) cachedInstrumentFee(instrument string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rules[instrument].feeRate
}

func (a *Deribit) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func deribitLevels(raw [][]json.Number) ([]quicktrade.BookLevel, error) {
	levels := make([]quicktrade.BookLevel, 0, len(raw))
	for _, level := range raw {
		if len(level) != 2 {
			return nil, errors.New("order book level must contain price and quantity")
		}
		levels = append(levels, quicktrade.BookLevel{Price: level[0].String(), Quantity: level[1].String()})
	}
	return levels, nil
}
