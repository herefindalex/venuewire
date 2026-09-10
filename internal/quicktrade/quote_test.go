package quicktrade

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"venuewire/internal/domain"
)

func TestQuoteServiceMapsAllFourSpotDirectionsWithExactProtection(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	providers := fixtureProviders(now)
	tests := []struct {
		route, spend, instrument, side, qty, limit, gross, net, feeAsset string
		venue                                                            domain.Venue
	}{
		{"bybit-usdt-btc", "1000", "BTCUSDT", "Buy", "0.00995", "100500", "0.00995", "0.00994005", "BTC", domain.VenueBybit},
		{"bybit-btc-usdt", "0.01", "BTCUSDT", "Sell", "0.01", "99490.1", "999.9", "998.9001", "USDT", domain.VenueBybit},
		{"deribit-btc-eth", "0.01", "ETH_BTC", "Buy", "0.199", "0.0502", "0.199", "0.198801", "ETH", domain.VenueDeribit},
		{"deribit-eth-btc", "1", "ETH_BTC", "Sell", "1", "0.0497", "0.0499", "0.0498501", "BTC", domain.VenueDeribit},
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
		}, "100", "STALE_BOOK"},
		{"insufficient capacity", func(_ *Service, p map[domain.Venue]Provider) {
			p[domain.VenueBybit].(*fixtureProvider).capacity["USDT"] = "1"
		}, "100", "INSUFFICIENT_AVAILABLE_FUNDS"},
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

type fixtureProvider struct {
	market   MarketSnapshot
	capacity map[string]string
	fee      FeePolicy
	feeErr   error
}

func (p *fixtureProvider) Market(context.Context, Route) (MarketSnapshot, error) {
	return p.market, nil
}
func (p *fixtureProvider) Available(_ context.Context, _ Route, asset string) (Capacity, error) {
	return Capacity{Available: p.capacity[asset], AccountRevision: 7, ObservedAt: p.market.ObservedAt}, nil
}
func (p *fixtureProvider) Fee(context.Context, Route) (FeePolicy, error) { return p.fee, p.feeErr }

func fixtureProviders(now time.Time) map[domain.Venue]Provider {
	return map[domain.Venue]Provider{
		domain.VenueBybit: &fixtureProvider{
			market: MarketSnapshot{
				Rules: InstrumentRules{Instrument: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT", TickSize: "0.1", QuantityStep: "0.00001", MinimumQuantity: "0.00001", MinimumNotional: "5", MaximumQuantity: "10", MetadataRevision: "bybit-fixture-v1"},
				Bids:  []BookLevel{{Price: "99990", Quantity: "2"}}, Asks: []BookLevel{{Price: "100000", Quantity: "2"}}, ObservedAt: now,
			},
			capacity: map[string]string{"BTC": "10", "USDT": "1000000"},
			fee:      FeePolicy{Rate: "0.001", ChargeAsset: "to", Source: "fixture account fee"},
		},
		domain.VenueDeribit: &fixtureProvider{
			market: MarketSnapshot{
				Rules: InstrumentRules{Instrument: "ETH_BTC", BaseAsset: "ETH", QuoteAsset: "BTC", TickSize: "0.0001", QuantityStep: "0.001", MinimumQuantity: "0.001", MinimumNotional: "0.0001", MaximumQuantity: "100", MetadataRevision: "deribit-fixture-v1"},
				Bids:  []BookLevel{{Price: "0.0499", Quantity: "20"}}, Asks: []BookLevel{{Price: "0.05", Quantity: "20"}}, ObservedAt: now,
			},
			capacity: map[string]string{"BTC": "10", "ETH": "100"},
			fee:      FeePolicy{Rate: "0.001", ChargeAsset: "to", Source: "fixture account fee"},
		},
	}
}

func fixtureService(now time.Time, providers map[domain.Venue]Provider) *Service {
	return &Service{
		Providers: providers, TTL: 5 * time.Second, BookMaxAge: 3 * time.Second, SlippageBPS: 50,
		Caps: Caps{
			ByAsset:       map[string]string{"BTC": "0.01", "ETH": "1", "USDT": "1000"},
			ByVenueSource: map[string]string{"bybit:BTC": "0.01", "bybit:USDT": "1000", "deribit:BTC": "0.01", "deribit:ETH": "1"},
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
