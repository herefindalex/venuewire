package quicktrade

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/domain"
)

func TestQuoteServiceMapsAllSupportedSpotDirectionsWithExactProtection(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	providers := fixtureProviders(now)
	tests := []struct {
		route, spend, instrument, side, qty, limit, gross, net, feeAsset string
		venue                                                            domain.Venue
	}{
		{"bybit-usdt-btc", "1000", "BTCUSDT", "Buy", "0.00995", "100500", "0.00995", "0.00994005", "BTC", domain.VenueBybit},
		{"bybit-btc-usdt", "0.01", "BTCUSDT", "Sell", "0.01", "99490.1", "999.9", "998.9001", "USDT", domain.VenueBybit},
		{"bybit-usdt-eth", "1000", "ETHUSDT", "Buy", "0.402", "2484.96", "0.402", "0.401598", "ETH", domain.VenueBybit},
		{"bybit-eth-usdt", "1", "ETHUSDT", "Sell", "1", "2460.14", "2472.5", "2470.0275", "USDT", domain.VenueBybit},
		{"deribit-usdc-btc", "1000", "BTC_USDC", "Buy", "0.0127", "78490", "0.0127", "0.0127", "USDC", domain.VenueDeribit},
		{"deribit-btc-usdc", "0.01", "BTC_USDC", "Sell", "0.01", "77610", "780", "779.22", "USDC", domain.VenueDeribit},
	}
	for _, tc := range tests {
		t.Run(tc.route, func(t *testing.T) {
			service := fixtureService(now, providers)
			quote, err := service.Create(context.Background(), CreateRequest{Identity: "shared-user", Venue: tc.venue, RouteID: tc.route, SpendBudget: tc.spend, AccountAlias: string(tc.venue) + "-test"})
			if err != nil {
				t.Fatal(err)
			}
			if !quote.Executable || quote.Instrument != tc.instrument || quote.Side != tc.side || quote.BaseQty != tc.qty || quote.LimitPrice != tc.limit || quote.GrossReceiveEstimate != tc.gross || quote.NetReceiveEstimate != tc.net {
				t.Fatalf("quote = %+v", quote)
			}
			if quote.TimeInForce != "IOC" || quote.PriceProtectionBPS != 50 || !quote.ExpiresAt.Equal(now.Add(5*time.Second)) {
				t.Fatalf("protection/expiry = %+v", quote)
			}
			if len(quote.EstimatedFees) != 1 || quote.EstimatedFees[0].Asset != tc.feeAsset {
				t.Fatalf("fees = %+v", quote.EstimatedFees)
			}
		})
	}
}

func TestSourceAssetFeeStaysInsideSpendBudget(t *testing.T) {
	now := time.Now().UTC()
	providers := fixtureProviders(now)
	providers[domain.VenueBybit].(*fixtureProvider).fee = FeePolicy{Rate: "0.01", ChargeAsset: "from", Source: "fixture account fee"}
	service := fixtureService(now, providers)
	quote, err := service.Create(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "1000", AccountAlias: "bybit-test"})
	if err != nil {
		t.Fatal(err)
	}
	if quote.BaseQty != "0.00985" || quote.SourceDebitUpperBound != "999.82425" || quote.EstimatedFees[0].Asset != "USDT" || quote.EstimatedFees[0].EstimatedAmount != "9.89925" {
		t.Fatalf("fee-aware quote = %+v", quote)
	}
	if mustRat(t, quote.SourceDebitUpperBound).Cmp(mustRat(t, quote.SpendBudget)) > 0 {
		t.Fatalf("source debit exceeds budget: %+v", quote)
	}
}

func TestTradeQuoteCapsAndFreshnessFailClosed(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		mutate func(*Service, map[domain.Venue]Provider)
		spend  string
		code   string
	}{
		{"asset cap", func(s *Service, _ map[domain.Venue]Provider) { s.Caps.ByAsset["USDT"] = "99" }, "100", "DEMO_AMOUNT_LIMIT"},
		{"venue cap", func(s *Service, _ map[domain.Venue]Provider) { s.Caps.ByVenueSource["bybit:USDT"] = "99" }, "100", "DEMO_AMOUNT_LIMIT"},
		{"stale book", func(_ *Service, p map[domain.Venue]Provider) {
			p[domain.VenueBybit].(*fixtureProvider).market.ObservedAt = now.Add(-4 * time.Second)
		}, "100", "STALE_MARKET_DATA"},
		{"insufficient capacity", func(_ *Service, p map[domain.Venue]Provider) {
			p[domain.VenueBybit].(*fixtureProvider).capacity["USDT"] = "1"
		}, "100", "INSUFFICIENT_SPOT_BALANCE"},
		{"metadata mismatch", func(_ *Service, p map[domain.Venue]Provider) {
			p[domain.VenueBybit].(*fixtureProvider).market.Rules.BaseAsset = "ETH"
		}, "100", "METADATA_MISMATCH"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			providers := fixtureProviders(now)
			service := fixtureService(now, providers)
			tc.mutate(service, providers)
			_, err := service.Create(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: tc.spend, AccountAlias: "bybit-test"})
			assertQuoteCode(t, err, tc.code)
		})
	}
}

func TestUnknownFeeProducesNonExecutableReview(t *testing.T) {
	now := time.Now().UTC()
	providers := fixtureProviders(now)
	providers[domain.VenueBybit].(*fixtureProvider).feeErr = errors.New("fixture fee API unavailable")
	service := fixtureService(now, providers)
	quote, err := service.Create(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "100", AccountAlias: "bybit-test"})
	if err != nil {
		t.Fatal(err)
	}
	if quote.Executable || quote.BlockedReason != "FEE_MODEL_UNAVAILABLE" || len(quote.Warnings) == 0 {
		t.Fatalf("blocked quote = %+v", quote)
	}
}

func TestProviderFailuresHaveSafeStablePublicCodes(t *testing.T) {
	now := time.Now().UTC()
	providers := fixtureProviders(now)
	service := fixtureService(now, providers)
	provider := providers[domain.VenueBybit].(*fixtureProvider)
	privateCause := errors.New("private upstream diagnostic")
	provider.marketErr = privateCause
	_, err := service.Create(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "100", AccountAlias: "bybit-test"})
	assertQuoteCode(t, err, "MARKET_UNAVAILABLE")
	if !errors.Is(err, privateCause) || strings.Contains(err.Error(), "private upstream") {
		t.Fatalf("market error boundary = %q, unwrap=%t", err, errors.Is(err, privateCause))
	}

	provider.marketErr = nil
	provider.capacityErr = privateCause
	_, err = service.Create(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "100", AccountAlias: "bybit-test"})
	assertQuoteCode(t, err, "CAPACITY_UNAVAILABLE")
	if !errors.Is(err, privateCause) || strings.Contains(err.Error(), "private upstream") {
		t.Fatalf("capacity error boundary = %q, unwrap=%t", err, errors.Is(err, privateCause))
	}
}

func TestProtectedDepthWarnsWithoutIncreasingOrderQuantity(t *testing.T) {
	now := time.Now().UTC()
	providers := fixtureProviders(now)
	provider := providers[domain.VenueBybit].(*fixtureProvider)
	provider.market.Asks = []BookLevel{{Price: "100000", Quantity: "0.001"}, {Price: "101000", Quantity: "1"}}
	service := fixtureService(now, providers)
	quote, err := service.Create(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "1000", AccountAlias: "bybit-test"})
	if err != nil {
		t.Fatal(err)
	}
	if quote.BaseQty != "0.00995" || quote.GrossReceiveEstimate != "0.001" || len(quote.Warnings) != 2 {
		t.Fatalf("depth quote = %+v", quote)
	}
}

func TestQuoteServiceRequiresFreshValidTwoSidedBook(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name         string
		routeID      string
		spend        string
		bids         []BookLevel
		asks         []BookLevel
		wantCode     string
		wantEmptyRef string
	}{
		{name: "bids-only sell fails", routeID: "deribit-btc-usdc", spend: "0.001", bids: []BookLevel{{Price: "78000", Quantity: "2"}}, wantCode: "INVALID_BOOK"},
		{name: "bids-only buy fails", routeID: "deribit-usdc-btc", spend: "100", bids: []BookLevel{{Price: "78000", Quantity: "2"}}, wantCode: "INVALID_BOOK"},
		{name: "asks-only buy fails", routeID: "deribit-usdc-btc", spend: "100", asks: []BookLevel{{Price: "78100", Quantity: "2"}}, wantCode: "INVALID_BOOK"},
		{name: "asks-only sell fails", routeID: "deribit-btc-usdc", spend: "0.001", asks: []BookLevel{{Price: "78100", Quantity: "2"}}, wantCode: "INVALID_BOOK"},
		{name: "crossed two-sided book fails", routeID: "deribit-usdc-btc", spend: "100", bids: []BookLevel{{Price: "78100", Quantity: "2"}}, asks: []BookLevel{{Price: "78000", Quantity: "2"}}, wantCode: "INVALID_BOOK"},
		{name: "malformed executable side fails", routeID: "deribit-btc-usdc", spend: "0.001", bids: []BookLevel{{Price: "bad", Quantity: "2"}}, wantCode: "INVALID_BOOK"},
		{name: "valid two-sided book succeeds", routeID: "deribit-usdc-btc", spend: "100", bids: []BookLevel{{Price: "78000", Quantity: "2"}}, asks: []BookLevel{{Price: "78100", Quantity: "2"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			providers := fixtureProviders(now)
			provider := providers[domain.VenueDeribit].(*fixtureProvider)
			provider.market.Bids, provider.market.Asks = tc.bids, tc.asks
			quote, err := fixtureService(now, providers).Create(context.Background(), CreateRequest{
				Identity: "shared-user", Venue: domain.VenueDeribit, RouteID: tc.routeID,
				SpendBudget: tc.spend, AccountAlias: "deribit-test",
			})
			if tc.wantCode != "" {
				assertQuoteCode(t, err, tc.wantCode)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantEmptyRef == "ask" && quote.ReferenceAsk != "" {
				t.Fatalf("reference ask = %q, want empty", quote.ReferenceAsk)
			}
			if tc.wantEmptyRef == "bid" && quote.ReferenceBid != "" {
				t.Fatalf("reference bid = %q, want empty", quote.ReferenceBid)
			}
		})
	}
}

func TestValidateForConfirmRejectsChangedFeePolicy(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		mutate func(*FeePolicy)
	}{
		{name: "rate", mutate: func(fee *FeePolicy) { fee.Rate = "0.002" }},
		{name: "charge asset", mutate: func(fee *FeePolicy) { fee.ChargeAsset = "from" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			providers := fixtureProviders(now)
			provider := providers[domain.VenueBybit].(*fixtureProvider)
			service := fixtureService(now, providers)
			quote, err := service.Create(context.Background(), CreateRequest{
				Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc",
				SpendBudget: "100", AccountAlias: "bybit-test",
			})
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&provider.fee)
			assertQuoteCode(t, service.ValidateForConfirm(context.Background(), quote), "QUOTE_CHANGED")
		})
	}
}

type fixtureProvider struct {
	market      MarketSnapshot
	markets     map[string]MarketSnapshot
	marketErr   error
	capacity    map[string]string
	capacityErr error
	fee         FeePolicy
	fees        map[string]FeePolicy
	feeErr      error
}

func (p *fixtureProvider) Market(_ context.Context, route Route) (MarketSnapshot, error) {
	if market, ok := p.markets[route.ID]; ok {
		return market, p.marketErr
	}
	return p.market, p.marketErr
}
func (p *fixtureProvider) Available(_ context.Context, _ Route, asset string) (Capacity, error) {
	return Capacity{Available: p.capacity[asset], AccountRevision: 7, ObservedAt: p.market.ObservedAt}, p.capacityErr
}
func (p *fixtureProvider) Fee(_ context.Context, route Route) (FeePolicy, error) {
	if fee, ok := p.fees[route.ID]; ok {
		return fee, p.feeErr
	}
	return p.fee, p.feeErr
}

func fixtureProviders(now time.Time) map[domain.Venue]Provider {
	return map[domain.Venue]Provider{
		domain.VenueBybit: &fixtureProvider{
			market: MarketSnapshot{
				Rules: InstrumentRules{Instrument: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT", TickSize: "0.1", QuantityStep: "0.00001", MinimumQuantity: "0.00001", MinimumNotional: "5", MaximumQuantity: "10", MetadataRevision: "bybit-fixture-v1"},
				Bids:  []BookLevel{{Price: "99990", Quantity: "2"}}, Asks: []BookLevel{{Price: "100000", Quantity: "2"}}, ObservedAt: now,
			},
			capacity: map[string]string{"BTC": "10", "ETH": "100", "USDT": "1000000"},
			fee:      FeePolicy{Rate: "0.001", ChargeAsset: "to", Source: "fixture account fee"},
			markets: map[string]MarketSnapshot{
				"bybit-usdt-eth": {
					Rules: InstrumentRules{Instrument: "ETHUSDT", BaseAsset: "ETH", QuoteAsset: "USDT", TickSize: "0.01", QuantityStep: "0.001", MinimumQuantity: "0.001", MinimumNotional: "5", MaximumQuantity: "100", MetadataRevision: "bybit-eth-fixture-v1"},
					Bids:  []BookLevel{{Price: "2472.5", Quantity: "20"}}, Asks: []BookLevel{{Price: "2472.6", Quantity: "20"}}, ObservedAt: now,
				},
				"bybit-eth-usdt": {
					Rules: InstrumentRules{Instrument: "ETHUSDT", BaseAsset: "ETH", QuoteAsset: "USDT", TickSize: "0.01", QuantityStep: "0.001", MinimumQuantity: "0.001", MinimumNotional: "5", MaximumQuantity: "100", MetadataRevision: "bybit-eth-fixture-v1"},
					Bids:  []BookLevel{{Price: "2472.5", Quantity: "20"}}, Asks: []BookLevel{{Price: "2472.6", Quantity: "20"}}, ObservedAt: now,
				},
			},
		},
		domain.VenueDeribit: &fixtureProvider{
			market: MarketSnapshot{
				Rules: InstrumentRules{Instrument: "BTC_USDC", BaseAsset: "BTC", QuoteAsset: "USDC", TickSize: "1", QuantityStep: "0.0001", MinimumQuantity: "0.0001", MinimumNotional: "10", MaximumQuantity: "1", MetadataRevision: "deribit-fixture-v1"},
				Bids:  []BookLevel{{Price: "78000", Quantity: "2"}}, Asks: []BookLevel{{Price: "78100", Quantity: "2"}}, ObservedAt: now,
			},
			capacity: map[string]string{"BTC": "10", "USDC": "1000000"},
			fees: map[string]FeePolicy{
				"deribit-usdc-btc": {Rate: "0.001", ChargeAsset: "from", Source: "fixture account fee"},
				"deribit-btc-usdc": {Rate: "0.001", ChargeAsset: "to", Source: "fixture account fee"},
			},
		},
	}
}

func fixtureService(now time.Time, providers map[domain.Venue]Provider) *Service {
	return &Service{
		Providers: providers, TTL: 5 * time.Second, BookMaxAge: 3 * time.Second, SlippageBPS: 50,
		Caps: Caps{
			ByAsset:       map[string]string{"BTC": "0.01", "ETH": "1", "USDT": "1000", "USDC": "1000"},
			ByVenueSource: map[string]string{"bybit:BTC": "0.01", "bybit:ETH": "1", "bybit:USDT": "1000", "deribit:BTC": "0.01", "deribit:USDC": "1000"},
		},
		Now: func() time.Time { return now }, NewID: func() (string, error) { return "quote_fixture", nil },
	}
}

func assertQuoteCode(t *testing.T, err error, want string) {
	t.Helper()
	var quoteErr *Error
	if !errors.As(err, &quoteErr) || quoteErr.Code != want {
		t.Fatalf("error = %#v, want code %s", err, want)
	}
}

func mustRat(t *testing.T, value string) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		t.Fatalf("invalid decimal %q", value)
	}
	return r
}
