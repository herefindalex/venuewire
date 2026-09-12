package spotadapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/intent"
	"github.com/herefindalex/venuewire/internal/quicktrade"
	"github.com/herefindalex/venuewire/internal/rest"
)

type fakeCapacity struct {
	values map[domain.Venue]map[string]string
	asOf   time.Time
}

func (f *fakeCapacity) Capacity(venue domain.Venue, asset string) (string, uint64, time.Time, bool) {
	value, ok := f.values[venue][asset]
	return value, 7, f.asOf, ok
}

type fakeBybitClient struct {
	instrument      rest.Instrument
	book            rest.SpotOrderBook
	fee             rest.FeeRate
	capacity        rest.SpotBorrowCapacity
	ack             rest.OrderAck
	placeErr        error
	instrumentCalls int
	lastOrder       rest.PlaceOrderRequest
}

func (f *fakeBybitClient) Instruments(context.Context, string, string) ([]rest.Instrument, rest.ResponseMeta, error) {
	f.instrumentCalls++
	return []rest.Instrument{f.instrument}, rest.ResponseMeta{}, nil
}
func (f *fakeBybitClient) SpotOrderBook(context.Context, string, int) (rest.SpotOrderBook, rest.ResponseMeta, error) {
	return f.book, rest.ResponseMeta{}, nil
}
func (f *fakeBybitClient) SpotFeeRates(context.Context, string) ([]rest.FeeRate, rest.ResponseMeta, error) {
	return []rest.FeeRate{f.fee}, rest.ResponseMeta{}, nil
}
func (f *fakeBybitClient) SpotBorrowCapacity(context.Context, string, string) (rest.SpotBorrowCapacity, rest.ResponseMeta, error) {
	return f.capacity, rest.ResponseMeta{}, nil
}
func (f *fakeBybitClient) PlaceOrder(_ context.Context, request rest.PlaceOrderRequest) (rest.OrderAck, rest.ResponseMeta, error) {
	f.lastOrder = request
	return f.ack, rest.ResponseMeta{}, f.placeErr
}

type fakeDeribitClient struct {
	instrument      deribit.Instrument
	book            deribit.SpotOrderBook
	result          deribit.OrderResult
	placeErr        error
	instrumentCalls int
	lastSide        string
	lastOrder       deribit.PlaceParams
}

func (f *fakeDeribitClient) Instrument(context.Context, string) (deribit.Instrument, error) {
	f.instrumentCalls++
	return f.instrument, nil
}
func (f *fakeDeribitClient) SpotOrderBook(context.Context, string, int) (deribit.SpotOrderBook, error) {
	return f.book, nil
}
func (f *fakeDeribitClient) Place(_ context.Context, side string, request deribit.PlaceParams) (deribit.OrderResult, error) {
	f.lastSide, f.lastOrder = side, request
	return f.result, f.placeErr
}

func TestBybitAdapterNormalizesSpotInputsAndSubmission(t *testing.T) {
	now := time.UnixMilli(1789000000124).UTC()
	instrument := rest.Instrument{Symbol: "BTCUSDT", Status: "Trading", BaseCoin: "BTC", QuoteCoin: "USDT"}
	instrument.PriceFilter.TickSize = "0.01"
	instrument.LotSizeFilter.QtyStep = "0.000001"
	instrument.LotSizeFilter.MinOrderQty = "0.00001"
	instrument.LotSizeFilter.MaxOrderQty = "2"
	instrument.LotSizeFilter.MinOrderAmt = "5"
	client := &fakeBybitClient{
		instrument: instrument,
		book: rest.SpotOrderBook{Symbol: "BTCUSDT", TimestampMS: now.UnixMilli(),
			Bids: [][]string{{"99999.99", "0.2"}}, Asks: [][]string{{"100000.01", "0.3"}}},
		fee:      rest.FeeRate{Symbol: "BTCUSDT", TakerFeeRate: "0.001"},
		capacity: rest.SpotBorrowCapacity{Symbol: "BTCUSDT", Side: "Buy", MaxTradeAmount: "1000000", SpotMaxTradeAmount: "900"},
		ack:      rest.OrderAck{OrderID: "bybit-order-1"},
	}
	accounts := &fakeCapacity{values: map[domain.Venue]map[string]string{domain.VenueBybit: {"USDT": "1000"}}, asOf: now}
	adapter := &Bybit{Client: client, Accounts: accounts, MetadataTTL: time.Minute, Now: func() time.Time { return now }}
	route := routeByID(t, "bybit-usdt-btc")

	market, err := adapter.Market(context.Background(), route)
	if err != nil {
		t.Fatalf("Market() error = %v", err)
	}
	if market.Rules.MinimumNotional != "5" || market.Rules.QuantityStep != "0.000001" || market.Rules.MetadataRevision == "" || market.Bids[0].Price != "99999.99" || !market.ObservedAt.Equal(now) {
		t.Fatalf("Market() = %+v", market)
	}
	if _, err := adapter.Market(context.Background(), route); err != nil || client.instrumentCalls != 1 {
		t.Fatalf("metadata cache calls = %d, error = %v", client.instrumentCalls, err)
	}
	capacity, err := adapter.Available(context.Background(), route, "USDT")
	if err != nil || capacity.Available != "900" || capacity.AccountRevision != 7 {
		t.Fatalf("Available() = %+v, %v", capacity, err)
	}
	fee, err := adapter.Fee(context.Background(), route)
	if err != nil || fee.Rate != "0.001" || fee.ChargeAsset != "to" {
		t.Fatalf("Fee() = %+v, %v", fee, err)
	}

	trade := fixtureTrade(route, "Buy")
	submission, err := adapter.Submit(context.Background(), trade)
	if err != nil || submission.VenueOrderID != "bybit-order-1" || !submission.Accepted {
		t.Fatalf("Submit() = %+v, %v", submission, err)
	}
	if client.lastOrder.Category != "spot" || client.lastOrder.OrderType != "Limit" || client.lastOrder.TimeInForce != "IOC" || client.lastOrder.IsLeverage == nil || *client.lastOrder.IsLeverage != 0 || client.lastOrder.OrderLinkID != trade.ClientOrderID {
		t.Fatalf("native Bybit order = %+v", client.lastOrder)
	}
}

func TestBybitAdapterUsesQuantityStepWhenDeprecatedMinimumQuantityIsMissing(t *testing.T) {
	now := time.UnixMilli(1789000000124).UTC()
	instrument := rest.Instrument{Symbol: "BTCUSDT", Status: "Trading", BaseCoin: "BTC", QuoteCoin: "USDT"}
	instrument.PriceFilter.TickSize = "0.01"
	instrument.LotSizeFilter.QtyStep = "0.000001"
	instrument.LotSizeFilter.MinOrderAmt = "5"
	client := &fakeBybitClient{
		instrument: instrument,
		book: rest.SpotOrderBook{Symbol: "BTCUSDT", TimestampMS: now.UnixMilli(),
			Bids: [][]string{{"99999.99", "0.2"}}, Asks: [][]string{{"100000.01", "0.3"}}},
	}
	market, err := (&Bybit{Client: client, Now: func() time.Time { return now }}).Market(context.Background(), routeByID(t, "bybit-usdt-btc"))
	if err != nil {
		t.Fatal(err)
	}
	if market.Rules.MinimumQuantity != "0.000001" || market.Rules.MinimumNotional != "5" {
		t.Fatalf("minimum rules = %+v", market.Rules)
	}
}

func TestBybitAdapterClassifiesExplicitRejectionAndUncertainFailure(t *testing.T) {
	route := routeByID(t, "bybit-usdt-btc")
	client := &fakeBybitClient{placeErr: &rest.APIError{Code: 170140, Message: "order value exceeded"}}
	adapter := &Bybit{Client: client}
	if _, err := adapter.Submit(context.Background(), fixtureTrade(route, "Buy")); err == nil {
		t.Fatal("Submit() rejection error = nil")
	} else {
		var rejected *quicktrade.RejectedError
		if !errors.As(err, &rejected) {
			t.Fatalf("Submit() error = %T, want RejectedError", err)
		}
	}
	uncertain := &rest.UncertainSubmissionError{OrderLinkID: "vw-1", Cause: context.DeadlineExceeded}
	client.placeErr = uncertain
	if _, err := adapter.Submit(context.Background(), fixtureTrade(route, "Buy")); !errors.Is(err, uncertain) {
		t.Fatalf("Submit() uncertain error = %v, want unchanged", err)
	}
}

func TestDeribitAdapterNormalizesSpotInputsFeeAndSubmission(t *testing.T) {
	now := time.UnixMilli(1789000000124).UTC()
	client := &fakeDeribitClient{
		instrument: deribit.Instrument{InstrumentName: "BTC_USDC", Kind: "spot", BaseCurrency: "BTC", QuoteCurrency: "USDC", TickSize: json.Number("1"), AmountStep: json.Number("0.0001"), MinTradeAmount: json.Number("0.0001"), TakerCommission: json.Number("0.001"), IsActive: true},
		book:       deribit.SpotOrderBook{InstrumentName: "BTC_USDC", TimestampMS: now.UnixMilli(), Bids: [][]json.Number{{json.Number("78000"), json.Number("2")}}, Asks: [][]json.Number{{json.Number("78100"), json.Number("3")}}},
		result:     deribit.OrderResult{Order: deribit.Order{OrderID: "deribit-order-1", OrderState: "open"}},
	}
	accounts := &fakeCapacity{values: map[domain.Venue]map[string]string{domain.VenueDeribit: {"BTC": "0.02", "USDC": "1000"}}, asOf: now}
	adapter := &Deribit{Client: client, Accounts: accounts, MetadataTTL: time.Minute, Now: func() time.Time { return now }}
	route := routeByID(t, "deribit-usdc-btc")

	market, err := adapter.Market(context.Background(), route)
	if err != nil || market.Rules.TickSize != "1" || market.Rules.QuantityStep != "0.0001" || market.Rules.MetadataRevision == "" {
		t.Fatalf("Market() = %+v, %v", market, err)
	}
	fee, err := adapter.Fee(context.Background(), route)
	if err != nil || fee.Rate != "0.001" || fee.ChargeAsset != "from" {
		t.Fatalf("buy Fee() = %+v, %v", fee, err)
	}
	metadataDrivenRoute := route
	metadataDrivenRoute.QuoteAsset = "BTC"
	fee, err = adapter.Fee(context.Background(), metadataDrivenRoute)
	if err != nil || fee.ChargeAsset != "from" {
		t.Fatalf("Fee() must use API quote currency, got %+v, %v", fee, err)
	}
	sellRoute := routeByID(t, "deribit-btc-usdc")
	fee, err = adapter.Fee(context.Background(), sellRoute)
	if err != nil || fee.ChargeAsset != "to" || client.instrumentCalls != 1 {
		t.Fatalf("sell Fee() = %+v, calls=%d, error=%v", fee, client.instrumentCalls, err)
	}
	capacity, err := adapter.Available(context.Background(), route, "BTC")
	if err != nil || capacity.Available != "0.02" {
		t.Fatalf("Available() = %+v, %v", capacity, err)
	}

	trade := fixtureTrade(route, "Buy")
	submission, err := adapter.Submit(context.Background(), trade)
	if err != nil || submission.VenueOrderID != "deribit-order-1" || !submission.Accepted {
		t.Fatalf("Submit() = %+v, %v", submission, err)
	}
	if client.lastSide != "buy" || client.lastOrder.InstrumentName != "BTC_USDC" || client.lastOrder.Type != "limit" || client.lastOrder.TimeInForce != "immediate_or_cancel" || client.lastOrder.Label != trade.ClientOrderID || client.lastOrder.Amount.String() != trade.Quote.BaseQty {
		t.Fatalf("native Deribit order = %+v side=%q", client.lastOrder, client.lastSide)
	}
}

func TestDeribitAdapterRejectsUnverifiableMetadataAndClassifiesErrors(t *testing.T) {
	route := routeByID(t, "deribit-usdc-btc")
	client := &fakeDeribitClient{instrument: deribit.Instrument{
		InstrumentName: "BTC_USDC", Kind: "spot", BaseCurrency: "BTC", QuoteCurrency: "USDC", TickSize: json.Number("1"),
		AmountStep: json.Number(""), MinTradeAmount: json.Number("0.001"), TakerCommission: json.Number("0.001"), IsActive: true,
	}}
	adapter := &Deribit{Client: client}
	if _, err := adapter.Market(context.Background(), route); err == nil {
		t.Fatal("Market() with missing amount step error = nil")
	}

	client.placeErr = &deribit.RPCError{Code: 10004, Message: "price too low"}
	if _, err := adapter.Submit(context.Background(), fixtureTrade(route, "Buy")); err == nil {
		t.Fatal("Submit() rejection error = nil")
	} else {
		var rejected *quicktrade.RejectedError
		if !errors.As(err, &rejected) {
			t.Fatalf("Submit() error = %T, want RejectedError", err)
		}
	}
	unknown := &deribit.OutcomeUnknownError{Method: "private/buy", Cause: context.DeadlineExceeded}
	client.placeErr = unknown
	if _, err := adapter.Submit(context.Background(), fixtureTrade(route, "Buy")); !errors.Is(err, unknown) {
		t.Fatalf("Submit() uncertain error = %v, want unchanged", err)
	}
}

func routeByID(t *testing.T, id string) quicktrade.Route {
	t.Helper()
	for _, route := range quicktrade.Routes() {
		if route.ID == id {
			return route
		}
	}
	t.Fatalf("route %q not found", id)
	return quicktrade.Route{}
}

func fixtureTrade(route quicktrade.Route, side string) intent.QuickTrade {
	return intent.QuickTrade{
		ClientOrderID: "vw-fixture-1",
		Quote:         intent.QuickTradeQuote{Instrument: route.Instrument, Side: side, BaseQty: "0.01", LimitPrice: "100", TimeInForce: "IOC"},
	}
}
