package orderstate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"venuewire/internal/domain"
)

func TestRequiredStateTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct{ from, to domain.OrderStatus }{
		{domain.OrderStatusPendingSubmit, domain.OrderStatusNew},
		{domain.OrderStatusPendingSubmit, domain.OrderStatusRejected},
		{domain.OrderStatusNew, domain.OrderStatusPartiallyFilled},
		{domain.OrderStatusNew, domain.OrderStatusFilled},
		{domain.OrderStatusNew, domain.OrderStatusPendingCancel},
		{domain.OrderStatusPartiallyFilled, domain.OrderStatusFilled},
		{domain.OrderStatusPartiallyFilled, domain.OrderStatusPendingCancel},
		{domain.OrderStatusPendingCancel, domain.OrderStatusCancelled},
		{domain.OrderStatusPendingCancel, domain.OrderStatusFilled},
	}
	for _, tt := range tests {
		if err := ValidateTransition(tt.from, tt.to); err != nil {
			t.Errorf("%s -> %s rejected: %v", tt.from, tt.to, err)
		}
	}
}

func TestInvalidTransitionReturnsVisibleErrorWithoutMutation(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if err := service.ApplyOrder(ctx, fixtureOrder(domain.OrderStatusPendingSubmit)); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyOrder(ctx, fixtureOrder(domain.OrderStatusNew)); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyOrder(ctx, fixtureOrder(domain.OrderStatusFilled)); err != nil {
		t.Fatal(err)
	}
	err := service.ApplyOrder(ctx, fixtureOrder(domain.OrderStatusCancelled))
	var transition *TransitionError
	if !errors.As(err, &transition) {
		t.Fatalf("expected TransitionError, got %v", err)
	}
	if got := service.Snapshot().Orders["client-1"].Status; got != domain.OrderStatusFilled {
		t.Fatalf("invalid event mutated state to %s", got)
	}
}

func TestDuplicateExecutionDoesNotDoubleCount(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if err := service.ApplyOrder(ctx, fixtureOrder(domain.OrderStatusNew)); err != nil {
		t.Fatal(err)
	}
	execution := domain.Execution{ExecutionID: "exec-1", OrderID: "exchange-1", OrderLinkID: "client-1", Qty: "0.4", Price: "100.00", ReceivedAt: time.Now()}
	if err := service.ApplyExecution(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyExecution(ctx, execution); err != nil {
		t.Fatal(err)
	}
	order := service.Snapshot().Orders["client-1"]
	if order.CumFilledQty != "0.4" || order.Status != domain.OrderStatusPartiallyFilled || len(service.Snapshot().Executions) != 1 {
		t.Fatalf("duplicate corrupted state: %+v", order)
	}
}

func TestCancelFillRaceEndsFilled(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if err := service.ApplyOrder(ctx, fixtureOrder(domain.OrderStatusNew)); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyOrder(ctx, fixtureOrder(domain.OrderStatusPendingCancel)); err != nil {
		t.Fatal(err)
	}
	partial := domain.Execution{ExecutionID: "exec-partial", OrderID: "exchange-1", OrderLinkID: "client-1", Qty: "0.4", Price: "100", ReceivedAt: time.Now()}
	if err := service.ApplyExecution(ctx, partial); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Orders["client-1"].Status; got != domain.OrderStatusPendingCancel {
		t.Fatalf("partial fill erased pending cancel: %s", got)
	}
	final := domain.Execution{ExecutionID: "exec-final", OrderID: "exchange-1", OrderLinkID: "client-1", Qty: "0.6", Price: "100", ReceivedAt: time.Now()}
	if err := service.ApplyExecution(ctx, final); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Orders["client-1"].Status; got != domain.OrderStatusFilled {
		t.Fatalf("final fill did not win race: %s", got)
	}
}

func TestCancelledThenConfirmedFullFillEndsFilled(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	for _, status := range []domain.OrderStatus{domain.OrderStatusNew, domain.OrderStatusPendingCancel, domain.OrderStatusCancelled} {
		if err := service.ApplyOrder(ctx, fixtureOrder(status)); err != nil {
			t.Fatal(err)
		}
	}
	final := domain.Execution{ExecutionID: "exec-final", OrderID: "exchange-1", OrderLinkID: "client-1", Qty: "1.0", Price: "100", ReceivedAt: time.Now()}
	if err := service.ApplyExecution(ctx, final); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Orders["client-1"].Status; got != domain.OrderStatusFilled {
		t.Fatalf("final fill did not override cancelled race: %s", got)
	}
}

func TestServicePersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	store := FileStore{Path: path}
	service, err := NewService(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyOrder(context.Background(), fixtureOrder(domain.OrderStatusNew)); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewService(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot().Orders["client-1"].Status; got != domain.OrderStatusNew {
		t.Fatalf("reloaded status = %s", got)
	}
}

func TestServiceIsolatesIdenticalOrderAndExecutionIDsAcrossVenues(t *testing.T) {
	ctx := context.Background()
	service, err := NewService(ctx, FileStore{Path: filepath.Join(t.TempDir(), "orders.json")})
	if err != nil {
		t.Fatal(err)
	}
	orders := []domain.Order{
		{Exchange: "bybit", Environment: "testnet", AccountAlias: "bybit-test", Category: "linear", Symbol: "BTCUSDT", OrderID: "same-order", OrderLinkID: "same-link", Status: domain.OrderStatusNew},
		{Exchange: "deribit", Environment: "testnet", AccountAlias: "deribit-test", Category: "future", Symbol: "BTC-PERPETUAL", OrderID: "same-order", OrderLinkID: "same-link", Status: domain.OrderStatusNew},
	}
	for _, order := range orders {
		if err := service.ApplyOrder(ctx, order); err != nil {
			t.Fatalf("apply %+v: %v", order, err)
		}
	}
	for _, execution := range []domain.Execution{
		{Exchange: "bybit", Environment: "testnet", AccountAlias: "bybit-test", ExecutionID: "same-trade", OrderID: "same-order", OrderLinkID: "same-link", Qty: "1"},
		{Exchange: "deribit", Environment: "testnet", AccountAlias: "deribit-test", ExecutionID: "same-trade", OrderID: "same-order", OrderLinkID: "same-link", Qty: "2"},
	} {
		if err := service.ApplyExecution(ctx, execution); err != nil {
			t.Fatalf("apply execution %+v: %v", execution, err)
		}
	}
	snapshot := service.Snapshot()
	if len(snapshot.Orders) != 2 || len(snapshot.Executions) != 2 {
		t.Fatalf("cross-venue collision: %d orders, %d executions", len(snapshot.Orders), len(snapshot.Executions))
	}
	for _, order := range snapshot.Orders {
		if order.Exchange == "bybit" && order.CumFilledQty != "1" {
			t.Fatalf("Bybit fill contaminated: %+v", order)
		}
		if order.Exchange == "deribit" && order.CumFilledQty != "2" {
			t.Fatalf("Deribit fill contaminated: %+v", order)
		}
	}
}

func TestIndependentServicesDoNotLoseEachOthersOrders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	store := FileStore{Path: path}
	first, err := NewService(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewService(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	one := fixtureOrder(domain.OrderStatusNew)
	one.OrderID, one.OrderLinkID = "exchange-1", "client-1"
	two := fixtureOrder(domain.OrderStatusNew)
	two.OrderID, two.OrderLinkID = "exchange-2", "client-2"
	if err := first.ApplyOrder(context.Background(), one); err != nil {
		t.Fatal(err)
	}
	if err := second.ApplyOrder(context.Background(), two); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Orders) != 2 {
		t.Fatalf("concurrent-process update lost data: %+v", loaded.Orders)
	}
}

func TestLateRESTAckCannotRegressWebSocketState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	store := FileStore{Path: path}
	restProcess, err := NewService(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	streamProcess, err := NewService(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	wsOrder := fixtureOrder(domain.OrderStatusNew)
	wsOrder.RawStatus = "New"
	if err := streamProcess.ApplyOrder(context.Background(), wsOrder); err != nil {
		t.Fatal(err)
	}
	lateACK := fixtureOrder(domain.OrderStatusPendingSubmit)
	lateACK.RawStatus = "REST_ACK"
	if err := restProcess.UpsertREST(context.Background(), lateACK); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Orders["client-1"]
	if got.Status != domain.OrderStatusNew || got.RawStatus != "New" {
		t.Fatalf("late ACK regressed state: %+v", got)
	}
	filled := fixtureOrder(domain.OrderStatusFilled)
	filled.RawStatus = "Filled"
	filled.CumFilledQty = "1.0"
	if err := streamProcess.ApplyOrder(context.Background(), filled); err != nil {
		t.Fatal(err)
	}
	lateReject := fixtureOrder(domain.OrderStatusRejected)
	lateReject.RawStatus = "REST_REJECTED_10001"
	if err := restProcess.UpsertREST(context.Background(), lateReject); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got = loaded.Orders["client-1"]
	if got.Status != domain.OrderStatusFilled || got.RawStatus != "Filled" || got.CumFilledQty != "1.0" {
		t.Fatalf("late rejection regressed state: %+v", got)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, err := NewService(context.Background(), FileStore{Path: filepath.Join(t.TempDir(), "orders.json")})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func fixtureOrder(status domain.OrderStatus) domain.Order {
	return domain.Order{Exchange: "bybit", Category: "linear", Symbol: "BTCUSDT", OrderID: "exchange-1", OrderLinkID: "client-1", Qty: "1.0", Status: status, RawStatus: string(status), UpdatedAt: time.Now()}
}
