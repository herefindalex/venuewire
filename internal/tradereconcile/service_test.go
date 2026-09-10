package tradereconcile

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"venuewire/internal/deribit"
	"venuewire/internal/domain"
	"venuewire/internal/intent"
	"venuewire/internal/quicktrade"
	"venuewire/internal/rest"
)

type fakeBybit struct {
	orders     []rest.Order
	executions []rest.Execution
	err        error
	calls      int
}

func (f *fakeBybit) Orders(context.Context, string, string, string, string, *int) ([]rest.Order, rest.ResponseMeta, error) {
	f.calls++
	return f.orders, rest.ResponseMeta{}, f.err
}
func (f *fakeBybit) Executions(context.Context, string, string, string, string) ([]rest.Execution, rest.ResponseMeta, error) {
	f.calls++
	return f.executions, rest.ResponseMeta{}, f.err
}

type fakeDeribit struct {
	orders []deribit.Order
	trades []deribit.Trade
	err    error
	calls  int
}

func (f *fakeDeribit) OrderState(context.Context, string) (deribit.Order, error) {
	f.calls++
	if len(f.orders) == 0 {
		return deribit.Order{}, f.err
	}
	return f.orders[0], f.err
}
func (f *fakeDeribit) OrdersByLabel(context.Context, string, string) ([]deribit.Order, error) {
	f.calls++
	return f.orders, f.err
}
func (f *fakeDeribit) TradesByOrder(context.Context, string) ([]deribit.Trade, error) {
	f.calls++
	return f.trades, f.err
}

type fakeAccountRefresh struct {
	calls         []domain.Venue
	err           error
	statuses      []string
	discrepancies []bool
}

func (f *fakeAccountRefresh) UpdateReconciliation(_ domain.Venue, status string, discrepancy bool, _ time.Time) {
	f.statuses = append(f.statuses, status)
	f.discrepancies = append(f.discrepancies, discrepancy)
}

func (f *fakeAccountRefresh) Refresh(_ context.Context, venue domain.Venue) error {
	f.calls = append(f.calls, venue)
	return f.err
}

func TestBybitPartialIOCReconcilesFeesAndTerminalBalance(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store, trade := activeTrade(t, now, domain.VenueBybit, "Buy", "USDT", "BTC", "BTCUSDT", "1")
	client := &fakeBybit{
		orders: []rest.Order{{OrderID: "venue-1", OrderLinkID: trade.ClientOrderID, OrderStatus: "PartiallyFilledCanceled"}},
		executions: []rest.Execution{
			{OrderID: "venue-1", OrderLinkID: trade.ClientOrderID, ExecQty: "0.2", ExecPrice: "100", ExecFee: "0.0002", FeeCurrency: "BTC"},
			{OrderID: "venue-1", OrderLinkID: trade.ClientOrderID, ExecQty: "0.3", ExecPrice: "110", ExecFee: "0.0003", FeeCurrency: "BTC"},
		},
	}
	accounts := &fakeAccountRefresh{}
	service := &Service{Store: store, Bybit: client, Accounts: accounts}
	updated, err := service.RecheckTrade(context.Background(), trade.ID, now.Add(time.Second))
	if err != nil {
		t.Fatalf("RecheckTrade() error = %v", err)
	}
	if updated.Status != intent.TradeCancelled || updated.ResultStatus != "PARTIALLY_FILLED_CANCELLED" || updated.FilledBaseQty != "0.5" || updated.AveragePrice != "106" {
		t.Fatalf("reconciled trade = %+v", updated)
	}
	if updated.GrossSourceSpent != "53" || updated.GrossDestinationReceived != "0.5" || updated.NetDestinationReceived != "0.4995" || updated.ActualSourceDebit != "53" {
		t.Fatalf("reconciled amounts = %+v", updated)
	}
	if len(updated.Fees) != 1 || updated.Fees[0] != (intent.TradeFee{Asset: "BTC", Amount: "0.0005"}) || updated.BalanceSyncStatus != "SYNCED" {
		t.Fatalf("fees/balance = %+v %q", updated.Fees, updated.BalanceSyncStatus)
	}
	if len(accounts.calls) != 1 || accounts.calls[0] != domain.VenueBybit {
		t.Fatalf("account refresh calls = %v", accounts.calls)
	}
}

func TestDeribitZeroFillIOCIsTerminalAndReleasesSlot(t *testing.T) {
	now := time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC)
	store, trade := activeTrade(t, now, domain.VenueDeribit, "Sell", "ETH", "BTC", "ETH_BTC", "1")
	client := &fakeDeribit{orders: []deribit.Order{{OrderID: "venue-2", Label: trade.ClientOrderID, InstrumentName: "ETH_BTC", OrderState: "cancelled"}}}
	service := &Service{Store: store, Deribit: client}
	updated, err := service.RecheckTrade(context.Background(), trade.ID, now.Add(time.Second))
	if err != nil {
		t.Fatalf("RecheckTrade() error = %v", err)
	}
	if updated.Status != intent.TradeCancelled || updated.ResultStatus != "CANCELLED_NO_FILL" || updated.FilledBaseQty != "0" || updated.FillDetailsStatus != "COMPLETE" || updated.FeeDetailsStatus != "COMPLETE" {
		t.Fatalf("zero-fill trade = %+v", updated)
	}
	_, _, err = store.ConfirmQuickTrade(context.Background(), confirmation("next", now.Add(2*time.Second), domain.VenueDeribit, "Sell", "ETH", "BTC", "ETH_BTC", "1"), intent.DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 10, MaxConcurrentTrades: 1}, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("terminal trade did not release concurrent slot: %v", err)
	}
}

func TestUnknownWithoutEvidenceStaysActiveAndRetainsSlot(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)
	store, trade := activeTrade(t, now, domain.VenueBybit, "Sell", "BTC", "USDT", "BTCUSDT", "1")
	if _, err := store.UpdateQuickTrade(context.Background(), trade.ID, intent.QuickTradeUpdate{Status: intent.TradeUnknown, PublicError: "Outcome unknown."}, now); err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: store, Bybit: &fakeBybit{}}
	updated, err := service.RecheckTrade(context.Background(), trade.ID, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != intent.TradeUnknown || updated.ResultStatus != "OUTCOME_UNKNOWN" || updated.LastCheckedAt.IsZero() || updated.LastPublicError == "" {
		t.Fatalf("unknown trade = %+v", updated)
	}
	_, _, err = store.ConfirmQuickTrade(context.Background(), confirmation("next", now.Add(2*time.Second), domain.VenueBybit, "Sell", "BTC", "USDT", "BTCUSDT", "1"), intent.DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 10, MaxConcurrentTrades: 1}, now.Add(2*time.Second))
	var confirmErr *intent.ConfirmError
	if !errors.As(err, &confirmErr) || confirmErr.Code != "ACCOUNT_BUSY" {
		t.Fatalf("concurrent confirm error = %v", err)
	}
}

func TestUnknownReconcilesToEveryAuthoritativeTerminalOutcome(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 15, 0, 0, time.UTC)
	tests := []struct {
		name       string
		venueState string
		wantStatus intent.TradeStatus
		wantResult string
	}{
		{name: "filled", venueState: "Filled", wantStatus: intent.TradeFilled, wantResult: "FILLED"},
		{name: "cancelled", venueState: "Cancelled", wantStatus: intent.TradeCancelled, wantResult: "CANCELLED_NO_FILL"},
		{name: "rejected", venueState: "Rejected", wantStatus: intent.TradeRejected, wantResult: "REJECTED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, trade := activeTrade(t, now, domain.VenueBybit, "Buy", "USDT", "BTC", "BTCUSDT", "1")
			if _, err := store.UpdateQuickTrade(context.Background(), trade.ID, intent.QuickTradeUpdate{Status: intent.TradeUnknown, PublicError: "Outcome unknown."}, now); err != nil {
				t.Fatal(err)
			}
			accounts := &fakeAccountRefresh{}
			service := &Service{Store: store, Bybit: &fakeBybit{orders: []rest.Order{{
				OrderID: trade.VenueOrderID, OrderLinkID: trade.ClientOrderID, OrderStatus: test.venueState,
			}}}, Accounts: accounts}
			updated, err := service.RecheckTrade(context.Background(), trade.ID, now.Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if updated.Status != test.wantStatus || updated.ResultStatus != test.wantResult || updated.LastPublicError != "" || updated.TerminalAt == nil {
				t.Fatalf("reconciled trade = %+v", updated)
			}
			if !slices.Equal(accounts.statuses, []string{"RUNNING", "SYNCED"}) || !slices.Equal(accounts.discrepancies, []bool{false, true}) {
				t.Fatalf("reconciliation observations = statuses %v discrepancies %v", accounts.statuses, accounts.discrepancies)
			}
		})
	}
}

func TestLaterFillEvidenceSupersedesCancelledState(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 30, 0, 0, time.UTC)
	store, trade := activeTrade(t, now, domain.VenueBybit, "Sell", "BTC", "USDT", "BTCUSDT", "1")
	if _, err := store.UpdateQuickTrade(context.Background(), trade.ID, intent.QuickTradeUpdate{Status: intent.TradeCancelled, ResultStatus: "CANCELLED_NO_FILL"}, now); err != nil {
		t.Fatal(err)
	}
	client := &fakeBybit{
		orders:     []rest.Order{{OrderID: "venue-1", OrderLinkID: trade.ClientOrderID, OrderStatus: "Filled"}},
		executions: []rest.Execution{{OrderID: "venue-1", OrderLinkID: trade.ClientOrderID, ExecQty: "1", ExecPrice: "100", ExecFee: "0.1", FeeCurrency: "USDT"}},
	}
	accounts := &fakeAccountRefresh{}
	updated, err := (&Service{Store: store, Bybit: client, Accounts: accounts}).RecheckTrade(context.Background(), trade.ID, now.Add(time.Second))
	if err != nil {
		t.Fatalf("RecheckTrade() error = %v", err)
	}
	if updated.Status != intent.TradeFilled || updated.ResultStatus != "FILLED" || updated.NetDestinationReceived != "99.9" || updated.TerminalAt == nil {
		t.Fatalf("fill superseding cancellation = %+v", updated)
	}
	if !slices.Equal(accounts.statuses, []string{"RUNNING", "SYNCED"}) || !slices.Equal(accounts.discrepancies, []bool{false, true}) {
		t.Fatalf("reconciliation observations = %v %v", accounts.statuses, accounts.discrepancies)
	}
}

func TestStartupRecoveryNeverSubmitsCreatedIntent(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	store := intent.Store{Path: t.TempDir() + "/intents.json"}
	created, _, err := store.ConfirmQuickTrade(context.Background(), confirmation("created", now, domain.VenueBybit, "Buy", "USDT", "BTC", "BTCUSDT", "1"), intent.DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 10, MaxConcurrentTrades: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	venue := &fakeBybit{}
	service := &Service{Store: store, Bybit: venue}
	if err := service.Recover(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetQuickTrade(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != intent.TradeRejected || updated.ResultStatus != "NOT_SUBMITTED_AFTER_RESTART" || venue.calls != 0 {
		t.Fatalf("recovered trade = %+v calls=%d", updated, venue.calls)
	}
}

func TestRecheckFailureIsSafeAndDoesNotMutateTrade(t *testing.T) {
	now := time.Date(2026, 9, 10, 16, 0, 0, 0, time.UTC)
	store, trade := activeTrade(t, now, domain.VenueBybit, "Buy", "USDT", "BTC", "BTCUSDT", "1")
	cause := errors.New("private venue diagnostic")
	service := &Service{Store: store, Bybit: &fakeBybit{err: cause}}
	_, err := service.RecheckTrade(context.Background(), trade.ID, now.Add(time.Second))
	var public *quicktrade.Error
	if !errors.As(err, &public) || public.Code != "VENUE_RECOVERING" || !errors.Is(err, cause) || err.Error() != "Venue evidence could not be checked yet." {
		t.Fatalf("recheck error = %#v", err)
	}
	unchanged, _ := store.GetQuickTrade(context.Background(), trade.ID)
	if unchanged.LastCheckedAt != nil {
		t.Fatalf("failed recheck mutated trade: %+v", unchanged)
	}
}

func activeTrade(t *testing.T, now time.Time, venue domain.Venue, side, from, to, instrumentName, qty string) (intent.Store, intent.QuickTrade) {
	t.Helper()
	store := intent.Store{Path: t.TempDir() + "/intents.json"}
	created, _, err := store.ConfirmQuickTrade(context.Background(), confirmation("active", now, venue, side, from, to, instrumentName, qty), intent.DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 10, MaxConcurrentTrades: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	created, err = store.MarkQuickTradeDispatching(context.Background(), created.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	created, err = store.UpdateQuickTrade(context.Background(), created.ID, intent.QuickTradeUpdate{Status: intent.TradeAccepted, VenueOrderID: map[domain.Venue]string{domain.VenueBybit: "venue-1", domain.VenueDeribit: "venue-2"}[venue]}, now)
	if err != nil {
		t.Fatal(err)
	}
	return store, created
}

func confirmation(id string, now time.Time, venue domain.Venue, side, from, to, instrumentName, qty string) intent.QuickTradeConfirmation {
	return intent.QuickTradeConfirmation{IntentID: "trade-" + id, Identity: "shared", SessionID: "session", ClientRequestID: "request-" + id, ClientOrderID: "vw-" + id,
		Quote: intent.QuickTradeQuote{ID: "quote-" + id, Identity: "shared", Venue: string(venue), Environment: "testnet", AccountAlias: string(venue) + "-test", FromAsset: from, ToAsset: to, Instrument: instrumentName, Side: side, BaseQty: qty, LimitPrice: "100", TimeInForce: "IOC", CreatedAt: now, ExpiresAt: now.Add(time.Minute), Executable: true}}
}
