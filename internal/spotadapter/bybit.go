package spotadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"venuewire/internal/domain"
	"venuewire/internal/intent"
	"venuewire/internal/quicktrade"
	"venuewire/internal/rest"
)

type CapacityReader interface {
	Capacity(domain.Venue, string) (string, uint64, time.Time, bool)
}

type BybitClient interface {
	Instruments(context.Context, string, string) ([]rest.Instrument, rest.ResponseMeta, error)
	SpotOrderBook(context.Context, string, int) (rest.SpotOrderBook, rest.ResponseMeta, error)
	SpotFeeRates(context.Context, string) ([]rest.FeeRate, rest.ResponseMeta, error)
	SpotBorrowCapacity(context.Context, string, string) (rest.SpotBorrowCapacity, rest.ResponseMeta, error)
	PlaceOrder(context.Context, rest.PlaceOrderRequest) (rest.OrderAck, rest.ResponseMeta, error)
}

type Bybit struct {
	Client      BybitClient
	Accounts    CapacityReader
	MetadataTTL time.Duration
	Now         func() time.Time

	mu    sync.Mutex
	rules map[string]cachedRules
}

type cachedRules struct {
	value     quicktrade.InstrumentRules
	expiresAt time.Time
	feeRate   string
}

func (a *Bybit) Market(ctx context.Context, route quicktrade.Route) (quicktrade.MarketSnapshot, error) {
	if a.Client == nil {
		return quicktrade.MarketSnapshot{}, errors.New("Bybit Spot client is unavailable")
	}
	rules, err := a.instrumentRules(ctx, route)
	if err != nil {
		return quicktrade.MarketSnapshot{}, err
	}
	book, _, err := a.Client.SpotOrderBook(ctx, route.Instrument, 50)
	if err != nil {
		return quicktrade.MarketSnapshot{}, err
	}
	if book.Symbol != route.Instrument || book.TimestampMS <= 0 {
		return quicktrade.MarketSnapshot{}, errors.New("Bybit Spot order book scope or timestamp is invalid")
	}
	bids, err := bybitLevels(book.Bids)
	if err != nil {
		return quicktrade.MarketSnapshot{}, fmt.Errorf("Bybit Spot bids: %w", err)
	}
	asks, err := bybitLevels(book.Asks)
	if err != nil {
		return quicktrade.MarketSnapshot{}, fmt.Errorf("Bybit Spot asks: %w", err)
	}
	return quicktrade.MarketSnapshot{Rules: rules, Bids: bids, Asks: asks, ObservedAt: time.UnixMilli(book.TimestampMS).UTC()}, nil
}

func (a *Bybit) Available(ctx context.Context, route quicktrade.Route, asset string) (quicktrade.Capacity, error) {
	if a.Accounts == nil {
		return quicktrade.Capacity{}, errors.New("Bybit account capacity is unavailable")
	}
	available, accountRevision, observedAt, ok := a.Accounts.Capacity(domain.VenueBybit, strings.ToUpper(strings.TrimSpace(asset)))
	if !ok {
		return quicktrade.Capacity{}, errors.New("Bybit account capacity is stale or unknown")
	}
	if !strings.EqualFold(asset, route.FromAsset) {
		return quicktrade.Capacity{Available: available, AccountRevision: accountRevision, ObservedAt: observedAt}, nil
	}
	if a.Client == nil {
		return quicktrade.Capacity{}, errors.New("Bybit Spot client is unavailable")
	}
	capacity, _, err := a.Client.SpotBorrowCapacity(ctx, route.Instrument, string(route.Side))
	if err != nil {
		return quicktrade.Capacity{}, err
	}
	if capacity.Symbol != route.Instrument || capacity.Side != string(route.Side) {
		return quicktrade.Capacity{}, errors.New("Bybit Spot capacity scope is invalid")
	}
	spotCapacity := capacity.SpotMaxTradeQty
	if route.Side == domain.SideBuy {
		spotCapacity = capacity.SpotMaxTradeAmount
	}
	available, err = minimumDecimal(available, spotCapacity)
	if err != nil {
		return quicktrade.Capacity{}, err
	}
	return quicktrade.Capacity{Available: available, AccountRevision: accountRevision, ObservedAt: observedAt}, nil
}

func (a *Bybit) Fee(ctx context.Context, route quicktrade.Route) (quicktrade.FeePolicy, error) {
	if a.Client == nil {
		return quicktrade.FeePolicy{}, errors.New("Bybit Spot client is unavailable")
	}
	rates, _, err := a.Client.SpotFeeRates(ctx, route.Instrument)
	if err != nil {
		return quicktrade.FeePolicy{}, err
	}
	if len(rates) != 1 || rates[0].Symbol != route.Instrument {
		return quicktrade.FeePolicy{}, errors.New("Bybit Spot fee rate scope is invalid")
	}
	if _, ok := nonNegativeDecimal(rates[0].TakerFeeRate); !ok {
		return quicktrade.FeePolicy{}, errors.New("Bybit Spot taker fee is invalid")
	}
	return quicktrade.FeePolicy{Rate: rates[0].TakerFeeRate, ChargeAsset: "to", Source: "Bybit account Spot taker fee"}, nil
}

func (a *Bybit) Submit(ctx context.Context, trade intent.QuickTrade) (quicktrade.Submission, error) {
	if a.Client == nil {
		return quicktrade.Submission{}, errors.New("Bybit Spot client is unavailable")
	}
	zero := 0
	ack, _, err := a.Client.PlaceOrder(ctx, rest.PlaceOrderRequest{
		Category: "spot", Symbol: trade.Quote.Instrument, Side: trade.Quote.Side, OrderType: "Limit",
		Qty: trade.Quote.BaseQty, Price: trade.Quote.LimitPrice, TimeInForce: "IOC",
		OrderLinkID: trade.ClientOrderID, IsLeverage: &zero,
	})
	if err != nil {
		var rejected *rest.APIError
		if errors.As(err, &rejected) {
			return quicktrade.Submission{}, &quicktrade.RejectedError{PublicMessage: "Bybit rejected the Spot order.", Err: err}
		}
		return quicktrade.Submission{}, err
	}
	if strings.TrimSpace(ack.OrderID) == "" {
		return quicktrade.Submission{}, errors.New("Bybit acknowledged the request without an order ID")
	}
	return quicktrade.Submission{VenueOrderID: ack.OrderID, RawVenueStatus: "ACKNOWLEDGED", Accepted: true}, nil
}

func (a *Bybit) instrumentRules(ctx context.Context, route quicktrade.Route) (quicktrade.InstrumentRules, error) {
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

	instruments, _, err := a.Client.Instruments(ctx, "spot", route.Instrument)
	if err != nil {
		return quicktrade.InstrumentRules{}, err
	}
	if len(instruments) != 1 {
		return quicktrade.InstrumentRules{}, errors.New("Bybit Spot instrument metadata is unavailable")
	}
	instrument := instruments[0]
	if instrument.Symbol != route.Instrument || instrument.Status != "Trading" {
		return quicktrade.InstrumentRules{}, errors.New("Bybit Spot instrument is not active")
	}
	minimumNotional := instrument.LotSizeFilter.MinOrderAmt
	if minimumNotional == "" {
		minimumNotional = instrument.LotSizeFilter.MinNotional
	}
	rules := quicktrade.InstrumentRules{
		Instrument: instrument.Symbol, BaseAsset: instrument.BaseCoin, QuoteAsset: instrument.QuoteCoin,
		TickSize: instrument.PriceFilter.TickSize, QuantityStep: instrument.LotSizeFilter.QtyStep,
		MinimumQuantity: instrument.LotSizeFilter.MinOrderQty, MinimumNotional: minimumNotional,
		MaximumQuantity: instrument.LotSizeFilter.MaxOrderQty,
	}
	if _, ok := positiveDecimal(rules.TickSize); !ok {
		return quicktrade.InstrumentRules{}, errors.New("Bybit Spot tick size is invalid")
	}
	if _, ok := positiveDecimal(rules.QuantityStep); !ok {
		return quicktrade.InstrumentRules{}, errors.New("Bybit Spot quantity step is invalid")
	}
	if _, ok := positiveDecimal(rules.MinimumQuantity); !ok {
		return quicktrade.InstrumentRules{}, errors.New("Bybit Spot minimum quantity is invalid")
	}
	rules.MetadataRevision = revision(rules.Instrument, rules.BaseAsset, rules.QuoteAsset, rules.TickSize, rules.QuantityStep, rules.MinimumQuantity, rules.MinimumNotional, rules.MaximumQuantity)
	a.mu.Lock()
	if a.rules == nil {
		a.rules = make(map[string]cachedRules)
	}
	a.rules[route.Instrument] = cachedRules{value: rules, expiresAt: now.Add(ttl)}
	a.mu.Unlock()
	return rules, nil
}

func (a *Bybit) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func bybitLevels(raw [][]string) ([]quicktrade.BookLevel, error) {
	levels := make([]quicktrade.BookLevel, 0, len(raw))
	for _, level := range raw {
		if len(level) != 2 {
			return nil, errors.New("order book level must contain price and quantity")
		}
		levels = append(levels, quicktrade.BookLevel{Price: level[0], Quantity: level[1]})
	}
	return levels, nil
}
