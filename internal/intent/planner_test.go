package intent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bybit/internal/deribit"
)

type fakeMarket struct {
	instrument           deribit.Instrument
	ticker               deribit.Ticker
	open                 map[string][]deribit.Order
	ack                  deribit.OrderResult
	state                deribit.Order
	placeErr, errorState error
	placeCalls           atomic.Int32
}

func (f *fakeMarket) Instrument(context.Context, string) (deribit.Instrument, error) {
	return f.instrument, nil
}
func (f *fakeMarket) Ticker(context.Context, string) (deribit.Ticker, error) { return f.ticker, nil }
func (f *fakeMarket) OpenFutureOrdersByCurrency(_ context.Context, c string) ([]deribit.Order, error) {
	return f.open[c], nil
}
func (f *fakeMarket) Place(context.Context, string, deribit.PlaceParams) (deribit.OrderResult, error) {
	f.placeCalls.Add(1)
	return f.ack, f.placeErr
}
func (f *fakeMarket) PlaceWS(context.Context, string, string, deribit.PlaceParams) (deribit.OrderResult, error) {
	f.placeCalls.Add(1)
	return f.ack, f.placeErr
}
func (f *fakeMarket) OrderState(context.Context, string) (deribit.Order, error) {
	return f.state, f.errorState
}

func validPlanner(t *testing.T) (*Planner, *fakeMarket, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	market := &fakeMarket{instrument: deribit.Instrument{InstrumentName: "BTC-PERPETUAL", Kind: "future", SettlementCurrency: "BTC", TickSize: json.Number("0.5"), MinTradeAmount: json.Number("10"), ContractSize: json.Number("10"), IsActive: true}, ticker: deribit.Ticker{InstrumentName: "BTC-PERPETUAL", MarkPrice: json.Number("80000")}, open: map[string][]deribit.Order{}, ack: deribit.OrderResult{Order: deribit.Order{OrderID: "native-1", Label: "intent-1", OrderState: "open"}}, state: deribit.Order{OrderID: "native-1", Label: "intent-1", OrderState: "open"}}
	planner := &Planner{Market: market, Store: Store{Path: filepath.Join(t.TempDir(), "intents.json")}, Limits: Limits{MaxOrderUSD: "100", MaxAggregateOpenUSD: "500", MaxPriceDeviationPct: "2", MaxOpenOrders: 5, TTL: 30 * time.Second}, AccountAlias: "deribit-test", Now: func() time.Time { return now }, NewID: func() (string, error) { return "intent-1", nil }}
	return planner, market, now
}

func TestPlanExecuteRequiresConfirmationAndIndependentRead(t *testing.T) {
	planner, market, _ := validPlanner(t)
	ctx := context.Background()
	plan, err := planner.Create(ctx, Request{Instrument: "BTC-PERPETUAL", Side: "buy", OrderType: "limit", Amount: "10", Price: "80000", TimeInForce: "good_til_cancelled", PostOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.AmountUnit != "USD_notional" || plan.Status != StatusPlanned {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.ValidTick != "0.5" || plan.EstimatedNotionalUSD != "10" || plan.MetadataAt.IsZero() || plan.PriceAt.IsZero() || plan.CODProtected {
		t.Fatalf("incomplete plan evidence: %+v", plan)
	}
	if _, err := planner.Execute(ctx, plan.ID, false); err == nil {
		t.Fatal("execution without confirm accepted")
	}
	result, err := planner.Execute(ctx, plan.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || market.placeCalls.Load() != 1 || result.ReadState.OrderID != "native-1" {
		t.Fatalf("result=%+v calls=%d", result, market.placeCalls.Load())
	}
	stored, _ := planner.Store.Get(ctx, plan.ID)
	if stored.Status != StatusSubmitted || stored.NativeOrderID != "native-1" {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestPlanRejectsMetadataAndRiskViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Planner, *fakeMarket, *Request)
	}{
		{"below minimum", func(_ *Planner, _ *fakeMarket, r *Request) { r.Amount = "1" }},
		{"amount increment", func(_ *Planner, _ *fakeMarket, r *Request) { r.Amount = "15" }},
		{"order limit", func(p *Planner, _ *fakeMarket, r *Request) { p.Limits.MaxOrderUSD = "9" }},
		{"tick", func(_ *Planner, _ *fakeMarket, r *Request) { r.Price = "80000.1" }},
		{"deviation", func(_ *Planner, _ *fakeMarket, r *Request) { r.Price = "82000" }},
		{"collateral", func(_ *Planner, m *fakeMarket, r *Request) { m.instrument.SettlementCurrency = "USDT" }},
		{"inactive", func(_ *Planner, m *fakeMarket, r *Request) { m.instrument.IsActive = false }},
		{"open count", func(p *Planner, m *fakeMarket, r *Request) {
			m.open["BTC"] = []deribit.Order{{Amount: json.Number("10")}, {Amount: json.Number("10")}, {Amount: json.Number("10")}, {Amount: json.Number("10")}, {Amount: json.Number("10")}}
		}},
		{"aggregate", func(p *Planner, m *fakeMarket, r *Request) {
			m.open["BTC"] = []deribit.Order{{Amount: json.Number("495")}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, m, _ := validPlanner(t)
			req := Request{Instrument: "BTC-PERPETUAL", Side: "buy", OrderType: "limit", Amount: "10", Price: "80000"}
			tt.mutate(p, m, &req)
			if _, err := p.Create(context.Background(), req); err == nil {
				t.Fatal("unsafe plan accepted")
			}
		})
	}
}

func TestExecuteMarksNeedsReviewWhenReadVerificationFails(t *testing.T) {
	planner, market, _ := validPlanner(t)
	plan, _ := planner.Create(context.Background(), Request{Instrument: "BTC-PERPETUAL", Side: "buy", OrderType: "limit", Amount: "10", Price: "80000"})
	market.errorState = errors.New("read unavailable")
	result, err := planner.Execute(context.Background(), plan.ID, true)
	if err == nil || result.Verified {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	stored, _ := planner.Store.Get(context.Background(), plan.ID)
	if stored.Status != StatusNeedsReview {
		t.Fatalf("status=%s", stored.Status)
	}
}

func TestConcurrentExecutionClaimsOnce(t *testing.T) {
	planner, market, _ := validPlanner(t)
	plan, _ := planner.Create(context.Background(), Request{Instrument: "BTC-PERPETUAL", Side: "buy", OrderType: "limit", Amount: "10", Price: "80000"})
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() { defer wait.Done(); _, _ = planner.Execute(context.Background(), plan.ID, true) }()
	}
	wait.Wait()
	if market.placeCalls.Load() != 1 {
		t.Fatalf("place calls=%d", market.placeCalls.Load())
	}
}
