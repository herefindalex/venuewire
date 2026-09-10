package fix

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"venuewire/internal/domain"
	"venuewire/internal/orderstate"
)

type bufferTransport struct{ bytes.Buffer }

func (*bufferTransport) Close() error { return nil }

func TestOrderRouterBuildsCurrentBybitRequestMessages(t *testing.T) {
	state, err := orderstate.NewService(context.Background(), orderstate.FileStore{Path: filepath.Join(t.TempDir(), "orders.json")})
	if err != nil {
		t.Fatal(err)
	}
	transport := &bufferTransport{}
	fixed := func() time.Time { return time.UnixMilli(1700000000000) }
	session := &Session{config: SessionConfig{SenderCompID: "FIX_CLIENT", TargetCompID: "BYBIT_FIX_SERVER", Now: fixed}, outboundSeq: 1, active: transport}
	router := &OrderRouter{Session: session, State: state, Now: fixed}
	if err := router.Place(context.Background(), NewOrderRequest{Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "1", Price: "100", ClOrdID: "client-1", TimeInForce: "GTC"}); err != nil {
		t.Fatal(err)
	}
	message, err := ParseStrict(transport.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	assertTags(t, message, map[int]string{35: "D", 30002: "1700000000000", 30001: "5000", 30010: "spot", 55: "BTCUSDT", 54: "1", 40: "2", 38: "1", 44: "100", 11: "client-1", 59: "1"})
	transport.Reset()
	if err := state.ApplyOrder(context.Background(), domain.Order{OrderID: "exchange-1", OrderLinkID: "client-1", Status: domain.OrderStatusNew, RawStatus: "New"}); err != nil {
		t.Fatal(err)
	}
	if err := router.Cancel(context.Background(), CancelRequest{Symbol: "BTCUSDT", OrderID: "exchange-1", ClOrdID: "client-1", ReqID: "req-cancel"}); err != nil {
		t.Fatal(err)
	}
	message, err = ParseStrict(transport.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	assertTags(t, message, map[int]string{35: "F", 30006: "req-cancel", 30010: "spot", 37: "exchange-1", 11: "client-1"})
	transport.Reset()
	if err := router.Amend(context.Background(), AmendRequest{Symbol: "BTCUSDT", OrderID: "exchange-1", ClOrdID: "client-1", Qty: "0.8", Price: "99", ReqID: "req-amend"}); err != nil {
		t.Fatal(err)
	}
	message, err = ParseStrict(transport.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	assertTags(t, message, map[int]string{35: "XAR", 30006: "req-amend", 30010: "spot", 37: "exchange-1", 38: "0.8", 44: "99"})
}

func TestCancelRejectWithoutIdentifiersUsesSendOrderCorrelation(t *testing.T) {
	state, err := orderstate.NewService(context.Background(), orderstate.FileStore{Path: filepath.Join(t.TempDir(), "orders.json")})
	if err != nil {
		t.Fatal(err)
	}
	order := domain.Order{OrderID: "exchange-1", OrderLinkID: "client-1", Status: domain.OrderStatusNew, RawStatus: "New"}
	if err := state.ApplyOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	order.Status, order.RawStatus = domain.OrderStatusPendingCancel, "PendingCancel"
	if err := state.ApplyOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	router := &OrderRouter{State: state, pendingCancel: []pendingRequest{{OrderID: "exchange-1", ClOrdID: "client-1", ReqID: "req-1"}}}
	message := Message{Fields: []Field{{35, "XCA"}, {103, "170213"}, {58, "Order does not exist"}}}
	if err := router.HandleMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if got := state.Snapshot().Orders["client-1"].Status; got != domain.OrderStatusRejectedCancel {
		t.Fatalf("status=%s", got)
	}
}

func TestAmendRejectWithoutIdentifiersUsesSendOrderCorrelationWithoutDisconnectError(t *testing.T) {
	state, err := orderstate.NewService(context.Background(), orderstate.FileStore{Path: filepath.Join(t.TempDir(), "orders.json")})
	if err != nil {
		t.Fatal(err)
	}
	order := domain.Order{OrderID: "exchange-1", OrderLinkID: "client-1", Status: domain.OrderStatusNew, RawStatus: "New"}
	if err := state.ApplyOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	router := &OrderRouter{State: state, pendingAmend: []pendingRequest{{OrderID: "exchange-1", ClOrdID: "client-1", ReqID: "req-1"}}}
	message := Message{Fields: []Field{{35, "XAA"}, {103, "10001"}, {58, "order remains unchanged"}}}
	if err := router.HandleMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	got := state.Snapshot().Orders["client-1"]
	if got.Status != domain.OrderStatusNew || got.RawStatus != "XAA_REJECTED:order remains unchanged" {
		t.Fatalf("order=%+v", got)
	}
}

func TestOrderValidationRejectsInvalidInputs(t *testing.T) {
	tests := []NewOrderRequest{{}, {Symbol: "BTCUSDT", Qty: "1", ClOrdID: "client-1", Side: "bad", OrderType: "Limit", Price: "1"}, {Symbol: "BTCUSDT", Qty: "1", ClOrdID: "client-1", Side: "Buy", OrderType: "Limit"}, {Symbol: "BTCUSDT", Qty: "1", ClOrdID: "abcdefghijklmnopqrstuvwxyz12345678901", Side: "Buy", OrderType: "Market"}}
	for _, request := range tests {
		if err := validateNewOrder(request); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
}

func assertTags(t *testing.T, message Message, want map[int]string) {
	t.Helper()
	for tag, value := range want {
		if got, ok := message.Get(tag); !ok || got != value {
			t.Errorf("tag %d=%q ok=%v want=%q", tag, got, ok, value)
		}
	}
}
