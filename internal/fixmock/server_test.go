package fixmock

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bybit/internal/domain"
	"bybit/internal/fix"
	"bybit/internal/orderstate"
)

func TestMockOrderScenarios(t *testing.T) {
	tests := []struct {
		name       string
		scenario   Scenario
		want       domain.OrderStatus
		executions int
	}{{"accepted", Accepted, domain.OrderStatusNew, 0}, {"rejected", Rejected, domain.OrderStatusRejected, 0}, {"partial then full", PartialThenFilled, domain.OrderStatusFilled, 2}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			session, router, state, mockDone := setup(t, ctx, tt.scenario)
			sessionDone := make(chan error, 1)
			go func() { sessionDone <- session.RunOnce(ctx) }()
			wait(t, func() bool { return session.Stats().Inbound >= 1 })
			if err := router.Place(ctx, fix.NewOrderRequest{Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "1.0", Price: "100", ClOrdID: "client-1", TimeInForce: "GTC"}); err != nil {
				t.Fatal(err)
			}
			wait(t, func() bool {
				return state.Snapshot().Orders["client-1"].Status == tt.want && len(state.Snapshot().Executions) == tt.executions
			})
			if len(state.Snapshot().Executions) != tt.executions {
				t.Fatalf("executions=%d", len(state.Snapshot().Executions))
			}
			shutdown(t, cancel, sessionDone, mockDone)
		})
	}
}

func TestMockCancelAcceptedAndCancelFillRace(t *testing.T) {
	for _, tt := range []struct {
		name     string
		scenario Scenario
		want     domain.OrderStatus
	}{{"cancelled", CancelAccepted, domain.OrderStatusCancelled}, {"fill wins", CancelRaceFilled, domain.OrderStatusFilled}} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			session, router, state, mockDone := setup(t, ctx, tt.scenario)
			sessionDone := make(chan error, 1)
			go func() { sessionDone <- session.RunOnce(ctx) }()
			wait(t, func() bool { return session.Stats().Inbound >= 1 })
			if err := router.Place(ctx, fix.NewOrderRequest{Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "1.0", Price: "100", ClOrdID: "client-1"}); err != nil {
				t.Fatal(err)
			}
			wait(t, func() bool { return state.Snapshot().Orders["client-1"].Status == domain.OrderStatusNew })
			if err := router.Cancel(ctx, fix.CancelRequest{Symbol: "BTCUSDT", ClOrdID: "client-1"}); err != nil {
				t.Fatal(err)
			}
			wait(t, func() bool { return state.Snapshot().Orders["client-1"].Status == tt.want })
			shutdown(t, cancel, sessionDone, mockDone)
		})
	}
}

func TestMockAmendUsesCurrentXARAndXAAFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	session, router, state, mockDone := setup(t, ctx, Accepted)
	sessionDone := make(chan error, 1)
	go func() { sessionDone <- session.RunOnce(ctx) }()
	wait(t, func() bool { return session.Stats().Inbound >= 1 })
	if err := router.Place(ctx, fix.NewOrderRequest{Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "1.0", Price: "100", ClOrdID: "client-1"}); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return state.Snapshot().Orders["client-1"].Status == domain.OrderStatusNew })
	if err := router.Amend(ctx, fix.AmendRequest{Symbol: "BTCUSDT", ClOrdID: "client-1", Qty: "0.8", Price: "99"}); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { o := state.Snapshot().Orders["client-1"]; return o.Qty == "0.8" && o.Price == "99" })
	shutdown(t, cancel, sessionDone, mockDone)
}

func setup(t *testing.T, ctx context.Context, scenario Scenario) (*fix.Session, *fix.OrderRouter, *orderstate.Service, <-chan error) {
	t.Helper()
	dial, mockDone := PairDialer(ctx, scenario)
	state, err := orderstate.NewService(ctx, orderstate.FileStore{Path: filepath.Join(t.TempDir(), "orders.json")})
	if err != nil {
		t.Fatal(err)
	}
	var router *fix.OrderRouter
	session, err := fix.NewSession(fix.SessionConfig{Dial: dial, SenderCompID: "FIX_CLIENT", APIKey: "fixture-key", Signer: testSigner{}, Heartbeat: time.Second, OnMessage: func(messageContext context.Context, message fix.Message) error {
		return router.HandleMessage(messageContext, message)
	}})
	if err != nil {
		t.Fatal(err)
	}
	router = &fix.OrderRouter{Session: session, State: state}
	return session, router, state, mockDone
}

type testSigner struct{}

func (testSigner) Sign([]byte) (string, error) { return "fixture-signature", nil }
func wait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met")
}
func shutdown(t *testing.T, cancel context.CancelFunc, sessionDone <-chan error, mockDone <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-sessionDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not stop")
	}
	select {
	case err := <-mockDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("mock did not stop")
	}
}
