package intent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestConfirmQuickTradeIsDurablyIdempotentAcrossRequestsAndExpiry(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "intents.json")}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	confirmation := quickConfirmation("intent-1", "session-1", "request-1", "quote-1", now)
	limits := DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 30, MaxConcurrentTrades: 1}

	first, created, err := store.ConfirmQuickTrade(context.Background(), confirmation, limits, now)
	if err != nil || !created {
		t.Fatalf("first confirm: trade=%+v created=%v err=%v", first, created, err)
	}
	retry, created, err := store.ConfirmQuickTrade(context.Background(), confirmation, limits, now.Add(2*time.Hour))
	if err != nil || created || retry.ID != first.ID {
		t.Fatalf("expired retry did not resolve existing intent: trade=%+v created=%v err=%v", retry, created, err)
	}

	secondRequest := confirmation
	secondRequest.ClientRequestID = "request-from-other-tab"
	retry, created, err = store.ConfirmQuickTrade(context.Background(), secondRequest, limits, now.Add(2*time.Hour))
	if err != nil || created || retry.ID != first.ID {
		t.Fatalf("consumed quote retry created another intent: trade=%+v created=%v err=%v", retry, created, err)
	}

	reloaded, err := (Store{Path: store.Path}).GetQuickTrade(context.Background(), first.ID)
	if err != nil || reloaded.ClientOrderID != "vw-intent-1" {
		t.Fatalf("reloaded trade = %+v err=%v", reloaded, err)
	}
}

func TestConfirmQuickTradeRejectsRequestIDConflict(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "intents.json")}
	now := time.Now().UTC()
	limits := DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 30, MaxConcurrentTrades: 2}
	first := quickConfirmation("intent-1", "session-1", "request-1", "quote-1", now)
	if _, _, err := store.ConfirmQuickTrade(context.Background(), first, limits, now); err != nil {
		t.Fatal(err)
	}
	conflict := quickConfirmation("intent-2", "session-1", "request-1", "quote-2", now)
	_, _, err := store.ConfirmQuickTrade(context.Background(), conflict, limits, now)
	var confirmErr *ConfirmError
	if !errors.As(err, &confirmErr) || confirmErr.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict error = %#v", err)
	}
}

func TestUnknownRetainsGlobalConcurrentSlotUntilResolved(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "intents.json")}
	now := time.Now().UTC()
	limits := DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 30, MaxConcurrentTrades: 1}
	first := quickConfirmation("intent-1", "session-1", "request-1", "quote-1", now)
	if _, _, err := store.ConfirmQuickTrade(context.Background(), first, limits, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkQuickTradeDispatching(context.Background(), first.IntentID, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateQuickTrade(context.Background(), first.IntentID, QuickTradeUpdate{Status: TradeUnknown, PublicError: "The exchange result could not be confirmed."}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	second := quickConfirmation("intent-2", "session-2", "request-2", "quote-2", now.Add(2*time.Second))
	_, _, err := store.ConfirmQuickTrade(context.Background(), second, limits, now.Add(2*time.Second))
	assertConfirmCode(t, err, "DEMO_TRADE_BUSY")

	if _, err := store.UpdateQuickTrade(context.Background(), first.IntentID, QuickTradeUpdate{Status: TradeFilled, ResultStatus: "FILLED", Checked: true}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.ConfirmQuickTrade(context.Background(), second, limits, now.Add(4*time.Second)); err != nil || !created {
		t.Fatalf("resolved intent did not release concurrency: created=%v err=%v", created, err)
	}
}

func TestSessionAndRollingHourLimitsUseAcceptedIntents(t *testing.T) {
	t.Run("session", func(t *testing.T) {
		store := Store{Path: filepath.Join(t.TempDir(), "intents.json")}
		now := time.Now().UTC()
		limits := DemoLimits{MaxTradesPerSession: 1, MaxTradesPerHour: 10, MaxConcurrentTrades: 1}
		first := quickConfirmation("intent-1", "session-1", "request-1", "quote-1", now)
		if _, _, err := store.ConfirmQuickTrade(context.Background(), first, limits, now); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpdateQuickTrade(context.Background(), first.IntentID, QuickTradeUpdate{Status: TradeRejected}, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		second := quickConfirmation("intent-2", "session-1", "request-2", "quote-2", now.Add(2*time.Second))
		_, _, err := store.ConfirmQuickTrade(context.Background(), second, limits, now.Add(2*time.Second))
		assertConfirmCode(t, err, "SESSION_TRADE_LIMIT")
	})

	t.Run("rolling hour survives restart", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "intents.json")
		store := Store{Path: path}
		now := time.Now().UTC()
		limits := DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 1, MaxConcurrentTrades: 1}
		first := quickConfirmation("intent-1", "session-1", "request-1", "quote-1", now)
		if _, _, err := store.ConfirmQuickTrade(context.Background(), first, limits, now); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpdateQuickTrade(context.Background(), first.IntentID, QuickTradeUpdate{Status: TradeRejected}, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		second := quickConfirmation("intent-2", "session-2", "request-2", "quote-2", now.Add(2*time.Second))
		_, _, err := (Store{Path: path}).ConfirmQuickTrade(context.Background(), second, limits, now.Add(2*time.Second))
		assertConfirmCode(t, err, "HOURLY_TRADE_LIMIT")

		later := now.Add(time.Hour + time.Second)
		second.Quote.CreatedAt, second.Quote.ExpiresAt = later, later.Add(time.Minute)
		if _, created, err := store.ConfirmQuickTrade(context.Background(), second, limits, later); err != nil || !created {
			t.Fatalf("rolling window did not expire: created=%v err=%v", created, err)
		}
	})
}

func TestFilledSupersedesPendingCancel(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "intents.json")}
	now := time.Now().UTC()
	limits := DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 30, MaxConcurrentTrades: 1}
	confirmation := quickConfirmation("intent-1", "session-1", "request-1", "quote-1", now)
	if _, _, err := store.ConfirmQuickTrade(context.Background(), confirmation, limits, now); err != nil {
		t.Fatal(err)
	}
	transitions := []TradeStatus{TradePendingSubmit, TradeSubmitted, TradeAccepted, TradePendingCancel, TradeFilled}
	for index, status := range transitions {
		if _, err := store.UpdateQuickTrade(context.Background(), confirmation.IntentID, QuickTradeUpdate{Status: status}, now.Add(time.Duration(index+1)*time.Second)); err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
	}
	trade, err := store.GetQuickTrade(context.Background(), confirmation.IntentID)
	if err != nil || trade.Status != TradeFilled || trade.TerminalAt == nil {
		t.Fatalf("trade = %+v err=%v", trade, err)
	}
}

func quickConfirmation(intentID, sessionID, requestID, quoteID string, now time.Time) QuickTradeConfirmation {
	return QuickTradeConfirmation{
		IntentID:        intentID,
		Identity:        "shared-demo-user",
		SessionID:       sessionID,
		ClientRequestID: requestID,
		ClientOrderID:   "vw-" + intentID,
		Quote: QuickTradeQuote{
			ID:                    quoteID,
			Identity:              "shared-demo-user",
			Venue:                 "bybit",
			Environment:           "testnet",
			AccountAlias:          "bybit-test",
			RouteID:               "bybit-usdt-btc",
			FromAsset:             "USDT",
			ToAsset:               "BTC",
			SpendBudget:           "100",
			Instrument:            "BTCUSDT",
			Side:                  "Buy",
			BaseQty:               "0.001",
			LimitPrice:            "100500",
			TimeInForce:           "IOC",
			ReferenceAsk:          "100000",
			PriceProtectionBPS:    50,
			SourceDebitUpperBound: "100",
			CreatedAt:             now,
			ExpiresAt:             now.Add(time.Minute),
			Executable:            true,
		},
	}
}

func assertConfirmCode(t *testing.T, err error, want string) {
	t.Helper()
	var confirmErr *ConfirmError
	if !errors.As(err, &confirmErr) || confirmErr.Code != want {
		t.Fatalf("error = %#v, want code %s", err, want)
	}
}
