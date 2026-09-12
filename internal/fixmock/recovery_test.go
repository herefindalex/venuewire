package fixmock

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/fix"
	"github.com/herefindalex/venuewire/internal/orderstate"
	"github.com/herefindalex/venuewire/internal/reconcile"
)

func TestDisconnectCreatesNewSessionAtOneAndReconciles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	dialCount := 0
	serverResults := make(chan error, 2)
	dial := func(context.Context) (fix.Transport, error) {
		mu.Lock()
		dialCount++
		current := dialCount
		mu.Unlock()
		if current > 2 {
			return nil, errors.New("unexpected third FIX session")
		}
		client, server := net.Pipe()
		scenario := Accepted
		if current == 1 {
			scenario = DisconnectAfterNew
		}
		go func() { serverResults <- New(scenario).Serve(ctx, server) }()
		return client, nil
	}
	state, err := orderstate.NewService(ctx, orderstate.FileStore{Path: filepath.Join(t.TempDir(), "orders.json")})
	if err != nil {
		t.Fatal(err)
	}
	remoteFinal := domain.Order{Exchange: "bybit", Category: "spot", Symbol: "BTCUSDT", OrderID: "mock-order-1", OrderLinkID: "client-recovery", Qty: "1.0", CumFilledQty: "1.0", Status: domain.OrderStatusFilled, RawStatus: "Filled", UpdatedAt: time.Now()}
	reconciler := reconcile.Reconciler{State: state, Exchange: recoveryReader{recent: []domain.Order{remoteFinal}}}
	var router *fix.OrderRouter
	session, err := fix.NewSession(fix.SessionConfig{Dial: dial, SenderCompID: "FIX_CLIENT", APIKey: "fixture-key", Signer: testSigner{}, Heartbeat: time.Second, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, OnMessage: func(messageContext context.Context, message fix.Message) error {
		return router.HandleMessage(messageContext, message)
	}, OnReconnect: func(reconnectContext context.Context) error {
		_, err := reconciler.Reconcile(reconnectContext)
		cancel()
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	router = &fix.OrderRouter{Session: session, State: state}
	done := make(chan error, 1)
	go func() { done <- session.Run(ctx) }()
	wait(t, func() bool { return session.Stats().Inbound >= 1 })
	if err := router.Place(ctx, fix.NewOrderRequest{Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "1.0", Price: "100", ClOrdID: "client-recovery"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session recovery did not complete")
	}
	if got := state.Snapshot().Orders["client-recovery"].Status; got != domain.OrderStatusFilled {
		t.Fatalf("reconciled status=%s", got)
	}
	if session.Stats().Reconnects != 1 {
		t.Fatalf("stats=%+v", session.Stats())
	}
	mu.Lock()
	gotDials := dialCount
	mu.Unlock()
	if gotDials != 2 {
		t.Fatalf("sessions=%d", gotDials)
	}
	firstErr := <-serverResults
	if !errors.Is(firstErr, io.EOF) {
		t.Fatalf("first mock error=%v", firstErr)
	}
	select {
	case secondErr := <-serverResults:
		if secondErr != nil && !errors.Is(secondErr, context.Canceled) {
			t.Fatalf("second mock error=%v", secondErr)
		}
	case <-time.After(time.Second):
		t.Fatal("second mock did not stop")
	}
}

type recoveryReader struct{ recent []domain.Order }

func (recoveryReader) OpenOrders(context.Context) ([]domain.Order, error)     { return nil, nil }
func (r recoveryReader) RecentOrders(context.Context) ([]domain.Order, error) { return r.recent, nil }
func (recoveryReader) Executions(context.Context) ([]domain.Execution, error) { return nil, nil }
