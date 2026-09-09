package ws

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrivateAuthSignatureKnownVector(t *testing.T) {
	if got, want := PrivateAuthSignature("test-secret", 1700000010000), "977d2a1068009c263a4e3e15a2838ccaf62d1eda4ca2ff08b5456910b481b58b"; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestPrivateQueueOverflowFailsConnectionForReconciliation(t *testing.T) {
	connection := newFakeConnection()
	connection.reads <- readResult{payload: []byte(`{"success":true,"ret_msg":"","op":"auth"}`)}
	for i := 0; i < 20; i++ {
		connection.reads <- readResult{payload: []byte(`{"id":"event","topic":"order.linear","creationTime":1700000000200,"data":[{"category":"linear","symbol":"BTCUSDT","orderId":"exchange-1","orderLinkId":"client-1","orderStatus":"New"}]}`)}
	}
	var calls atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := NewPrivateClient(PrivateConfig{
		URL: "fixture", APIKey: "key", APISecret: "secret", QueueSize: 1,
		InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		Dial: func(context.Context, string) (Connection, error) {
			if calls.Add(1) == 1 {
				return connection, nil
			}
			cancel()
			return nil, errors.New("stop fixture")
		},
		PingInterval: time.Hour, StaleAfter: time.Hour,
	})
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, func(sinkCtx context.Context, _ PrivateEvent) error { <-sinkCtx.Done(); return nil })
	}()
	waitFor(t, func() bool { return calls.Load() >= 2 })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() < 2 {
		t.Fatal("queue overflow did not fail and reconnect")
	}
}

func TestPrivateReconnectResubscribesAndReconciles(t *testing.T) {
	first := newFakeConnection()
	first.reads <- readResult{payload: []byte(`{"success":true,"ret_msg":"","op":"auth"}`)}
	first.reads <- readResult{err: errors.New("EOF")}
	second := newFakeConnection()
	second.reads <- readResult{payload: []byte(`{"success":true,"ret_msg":"","op":"auth"}`)}
	second.reads <- readResult{payload: []byte(`{"id":"event-1","topic":"order.linear","creationTime":1700000000200,"data":[{"category":"linear","symbol":"BTCUSDT","orderId":"exchange-1","orderLinkId":"client-1","orderStatus":"New"}]}`)}
	connections := make(chan *fakeConnection, 2)
	connections <- first
	connections <- second
	reconciled := make(chan struct{}, 1)
	client := NewPrivateClient(PrivateConfig{URL: "fixture", APIKey: "key", APISecret: "secret", Dial: func(context.Context, string) (Connection, error) { return <-connections, nil }, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, PingInterval: time.Hour, StaleAfter: time.Hour, OnReconnect: func(context.Context) error { reconciled <- struct{}{}; return nil }})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, func(_ context.Context, event PrivateEvent) error {
			if event.Order != nil {
				cancel()
			}
			return nil
		})
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-reconciled:
	default:
		t.Fatal("reconciliation callback not invoked")
	}
	if len(first.subscriptions()) != 2 || len(second.subscriptions()) != 3 {
		t.Fatalf("expected auth and subscribe on each connection: first=%d second=%d", len(first.subscriptions()), len(second.subscriptions()))
	}
	if second.deadlineCount() < 3 {
		t.Fatalf("private message did not refresh stale deadline: calls=%d", second.deadlineCount())
	}
}

func TestPrivateAuthenticationFailureNeverSubscribes(t *testing.T) {
	connection := newFakeConnection()
	connection.reads <- readResult{payload: []byte(`{"success":false,"ret_msg":"invalid sign","op":"auth"}`)}
	client := NewPrivateClient(PrivateConfig{URL: "fixture", APIKey: "key", APISecret: "secret", Dial: func(context.Context, string) (Connection, error) { return connection, nil }, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	err := client.Run(context.Background(), func(context.Context, PrivateEvent) error { return nil })
	var authErr *PrivateAuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error=%v", err)
	}
	if got := len(connection.subscriptions()); got != 1 {
		t.Fatalf("writes = %d, want only auth", got)
	}
}
