package deribitreconcile

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"venuewire/internal/deribit"
	"venuewire/internal/intent"
	"venuewire/internal/orderstate"
)

type fakeQueries struct {
	orders    map[string][]deribit.Order
	byOrder   map[string][]deribit.Trade
	pages     map[string][]deribit.TradePage
	pageCalls map[string]int
}

func (f *fakeQueries) OrdersByLabel(_ context.Context, _ string, label string) ([]deribit.Order, error) {
	return f.orders[label], nil
}
func (f *fakeQueries) TradesByOrder(_ context.Context, id string) ([]deribit.Trade, error) {
	return f.byOrder[id], nil
}
func (f *fakeQueries) TradesByInstrument(_ context.Context, instrument string, _ int64, _ int) (deribit.TradePage, error) {
	index := f.pageCalls[instrument]
	f.pageCalls[instrument]++
	list := f.pages[instrument]
	if index >= len(list) {
		return deribit.TradePage{}, nil
	}
	return list[index], nil
}

func newReconciler(t *testing.T, queries *fakeQueries) (*Reconciler, intent.Store, *orderstate.Service) {
	t.Helper()
	ctx := context.Background()
	intents := intent.Store{Path: filepath.Join(t.TempDir(), "intents.json")}
	state, err := orderstate.NewService(ctx, orderstate.FileStore{Path: filepath.Join(t.TempDir(), "orders.json")})
	if err != nil {
		t.Fatal(err)
	}
	return &Reconciler{Queries: queries, Intents: intents, State: state, AccountAlias: "deribit-test", Now: func() time.Time { return time.UnixMilli(3000) }}, intents, state
}

func TestRecoveryAdoptsExactlyOneOrderAndDeduplicatesTrades(t *testing.T) {
	trade := deribit.Trade{TradeID: "trade-1", OrderID: "native-1", InstrumentName: "BTC-PERPETUAL", Direction: "buy", Amount: json.Number("10"), Price: json.Number("80000"), Fee: json.Number("0.00001"), FeeCurrency: "BTC", Timestamp: 2000}
	queries := &fakeQueries{orders: map[string][]deribit.Order{"intent-1": {{OrderID: "native-1", Label: "intent-1", InstrumentName: "BTC-PERPETUAL", Direction: "buy", OrderType: "limit", OrderState: "filled", Amount: json.Number("10"), FilledAmount: json.Number("10"), Price: json.Number("80000"), AveragePrice: json.Number("80000"), CreationTimestamp: 1000, LastUpdateTimestamp: 2000}}}, byOrder: map[string][]deribit.Trade{"native-1": {trade}}, pages: map[string][]deribit.TradePage{"BTC-PERPETUAL": {{Trades: []deribit.Trade{trade}}}}, pageCalls: map[string]int{}}
	reconciler, intents, state := newReconciler(t, queries)
	plan := intent.Plan{ID: "intent-1", Venue: "deribit", Instrument: "BTC-PERPETUAL", Status: intent.StatusExecuting, ExpiresAt: time.Now().Add(time.Minute)}
	if err := intents.SavePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	report, err := reconciler.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Recovered != 1 || report.TradesApplied != 1 {
		t.Fatalf("report=%+v", report)
	}
	stored, _ := intents.Get(context.Background(), plan.ID)
	if stored.Status != intent.StatusSubmitted || stored.NativeOrderID != "native-1" {
		t.Fatalf("stored=%+v", stored)
	}
	snapshot := state.Snapshot()
	if len(snapshot.Orders) != 1 || len(snapshot.Executions) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	for _, execution := range snapshot.Executions {
		if execution.Fee != "0.00001" || execution.FeeCurrency != "BTC" {
			t.Fatalf("execution=%+v", execution)
		}
	}
	queries.pageCalls = map[string]int{}
	second, err := reconciler.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.TradesApplied != 0 || len(state.Snapshot().Executions) != 1 {
		t.Fatalf("second=%+v", second)
	}
}

func TestRecoveryNeverGuessesZeroOrMultipleMatches(t *testing.T) {
	queries := &fakeQueries{orders: map[string][]deribit.Order{"many": {{OrderID: "one"}, {OrderID: "two"}}}, byOrder: map[string][]deribit.Trade{}, pages: map[string][]deribit.TradePage{}, pageCalls: map[string]int{}}
	reconciler, intents, _ := newReconciler(t, queries)
	for _, plan := range []intent.Plan{{ID: "none", Venue: "deribit", Instrument: "BTC-PERPETUAL", Status: intent.StatusOutcomeUnknown}, {ID: "many", Venue: "deribit", Instrument: "BTC-PERPETUAL", Status: intent.StatusOutcomeUnknown}} {
		if err := intents.SavePlan(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
	}
	report, err := reconciler.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Unresolved != 1 || report.NeedsReview != 1 || report.Recovered != 0 {
		t.Fatalf("report=%+v", report)
	}
	none, _ := intents.Get(context.Background(), "none")
	many, _ := intents.Get(context.Background(), "many")
	if none.Status != intent.StatusOutcomeUnknown || many.Status != intent.StatusNeedsReview {
		t.Fatalf("none=%s many=%s", none.Status, many.Status)
	}
}

func TestPaginationSameMillisecondCursorAndNoProgressGuard(t *testing.T) {
	queries := &fakeQueries{orders: map[string][]deribit.Order{}, byOrder: map[string][]deribit.Trade{}, pages: map[string][]deribit.TradePage{"BTC-PERPETUAL": {{Trades: []deribit.Trade{{TradeID: "a", Timestamp: 1000}, {TradeID: "b", Timestamp: 1000}}, HasMore: true}, {Trades: []deribit.Trade{{TradeID: "b", Timestamp: 1000}}, HasMore: true}}}, pageCalls: map[string]int{}}
	reconciler, intents, _ := newReconciler(t, queries)
	_ = intents.SavePlan(context.Background(), intent.Plan{ID: "seed", Venue: "deribit", Instrument: "BTC-PERPETUAL", Status: intent.StatusPlanned})
	if _, err := reconciler.Run(context.Background()); err == nil {
		t.Fatal("pagination without progress accepted")
	}
	cursor, _ := intents.Cursor(context.Background(), "deribit|testnet|deribit-test|BTC-PERPETUAL")
	if cursor.Timestamp != 1000 || cursor.TradeID != "b" {
		t.Fatalf("cursor=%+v", cursor)
	}
}

func TestPrivateUserChangesUseCanonicalOrderAndTradeReducer(t *testing.T) {
	reconciler, _, state := newReconciler(t, &fakeQueries{})
	changes := deribit.UserChanges{
		Orders: []deribit.Order{{
			OrderID: "native-1", Label: "intent-1", InstrumentName: "BTC-PERPETUAL",
			Direction: "buy", OrderType: "limit", OrderState: "filled",
			Amount: json.Number("10"), FilledAmount: json.Number("10"), Price: json.Number("80000"),
			CreationTimestamp: 1000, LastUpdateTimestamp: 2000,
		}},
		Trades: []deribit.Trade{{
			TradeID: "trade-1", OrderID: "native-1", InstrumentName: "BTC-PERPETUAL",
			Direction: "buy", Amount: json.Number("10"), Price: json.Number("80000"),
			Fee: json.Number("-0.00000001"), FeeCurrency: "BTC", Timestamp: 2000,
		}},
		Positions: []deribit.Position{{InstrumentName: "BTC-PERPETUAL", Size: json.Number("10")}},
	}
	first, err := reconciler.ApplyUserChanges(context.Background(), changes)
	if err != nil {
		t.Fatal(err)
	}
	if first.OrdersApplied != 1 || first.TradesApplied != 1 || first.PositionsObserved != 1 {
		t.Fatalf("first report=%+v", first)
	}
	second, err := reconciler.ApplyUserChanges(context.Background(), changes)
	if err != nil {
		t.Fatal(err)
	}
	if second.TradesApplied != 0 || second.DuplicateOrExternalTrades != 1 {
		t.Fatalf("second report=%+v", second)
	}
	snapshot := state.Snapshot()
	if len(snapshot.Orders) != 1 || len(snapshot.Executions) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	for _, execution := range snapshot.Executions {
		if execution.Fee != "-0.00000001" || execution.FeeCurrency != "BTC" {
			t.Fatalf("execution=%+v", execution)
		}
	}
}
