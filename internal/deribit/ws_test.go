package deribit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"
)

type fakeWSRead struct {
	payload []byte
	err     error
}
type fakeWSConn struct {
	reads     chan fakeWSRead
	mu        sync.Mutex
	writes    []request
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeWS() *fakeWSConn {
	return &fakeWSConn{reads: make(chan fakeWSRead, 16), closed: make(chan struct{})}
}
func (f *fakeWSConn) ReadMessage() (int, []byte, error) {
	select {
	case <-f.closed:
		return 0, nil, io.EOF
	case read := <-f.reads:
		return 1, read.payload, read.err
	}
}
func (f *fakeWSConn) WriteJSON(value any) error {
	raw, _ := json.Marshal(value)
	var req request
	_ = json.Unmarshal(raw, &req)
	f.mu.Lock()
	f.writes = append(f.writes, req)
	f.mu.Unlock()
	return nil
}
func (f *fakeWSConn) SetReadDeadline(time.Time) error   { return nil }
func (f *fakeWSConn) SetPongHandler(func(string) error) {}
func (f *fakeWSConn) Close() error                      { f.closeOnce.Do(func() { close(f.closed) }); return nil }
func (f *fakeWSConn) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.writes))
	for i, w := range f.writes {
		out[i] = w.Method
	}
	return out
}
func wsPayload(value string) []byte { return []byte(value) }

func TestWSReconnectResubscribesAndRunsRecoveryBeforeEvents(t *testing.T) {
	first, second := newFakeWS(), newFakeWS()
	first.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":2,"result":["book.BTC-PERPETUAL.raw"]}`)}
	first.reads <- fakeWSRead{err: io.EOF}
	second.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":4,"result":["book.BTC-PERPETUAL.raw"]}`)}
	second.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","method":"subscription","params":{"channel":"book.BTC-PERPETUAL.raw","data":{"type":"snapshot"}}}`)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var dialCount int
	var ready []uint64
	client, err := NewWSClient(nil, WSConfig{URL: "wss://test.deribit.com/ws/api/v2", Channels: []string{"book.BTC-PERPETUAL.raw"}, ReconnectMin: time.Millisecond, ReconnectMax: time.Millisecond,
		Dial: func(context.Context, string) (WSConnection, error) {
			dialCount++
			if dialCount == 1 {
				return first, nil
			}
			return second, nil
		},
		OnReady: func(_ context.Context, g uint64) error { ready = append(ready, g); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, func(_ context.Context, event WSNotification) error {
			if event.Generation != 2 {
				t.Errorf("generation=%d", event.Generation)
			}
			cancel()
			return nil
		})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not stop")
	}
	if len(ready) != 2 || ready[0] != 1 || ready[1] != 2 {
		t.Fatalf("ready=%v", ready)
	}
	if client.Metrics().Reconnects != 1 {
		t.Fatalf("metrics=%+v", client.Metrics())
	}
	for _, conn := range []*fakeWSConn{first, second} {
		methods := conn.methods()
		if len(methods) < 2 || methods[0] != "public/set_heartbeat" || methods[1] != "public/subscribe" {
			t.Fatalf("writes=%v", methods)
		}
	}
}

func TestWSAnswersHeartbeatTestRequest(t *testing.T) {
	conn := newFakeWS()
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":2,"result":[]}`)}
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","method":"heartbeat","params":{"type":"test_request"}}`)}
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","method":"subscription","params":{"channel":"trades.BTC-PERPETUAL.raw","data":{}}}`)}
	ctx, cancel := context.WithCancel(context.Background())
	client, _ := NewWSClient(nil, WSConfig{URL: "wss://test.deribit.com/ws/api/v2", Channels: []string{"trades.BTC-PERPETUAL.raw"}, Dial: func(context.Context, string) (WSConnection, error) { return conn, nil }})
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, func(context.Context, WSNotification) error { cancel(); return nil }) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	found := false
	for _, method := range conn.methods() {
		if method == "public/test" {
			found = true
		}
	}
	if !found || client.Metrics().TestRequests != 1 {
		t.Fatalf("methods=%v metrics=%+v", conn.methods(), client.Metrics())
	}
}

func TestWSQueueOverflowFailsClosed(t *testing.T) {
	conn := newFakeWS()
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":2,"result":[]}`)}
	for range 4 {
		conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","method":"subscription","params":{"channel":"x","data":{}}}`)}
	}
	client, _ := NewWSClient(nil, WSConfig{URL: "wss://test.deribit.com/ws/api/v2", Channels: []string{"x"}, QueueSize: 1, Dial: func(context.Context, string) (WSConnection, error) { return conn, nil }})
	block := make(chan struct{})
	err := client.runConnection(context.Background(), conn, 1, func(context.Context, WSNotification) error { <-block; return nil })
	close(block)
	if err == nil || !errors.Is(err, err) || err.Error() != "Deribit WebSocket event queue full; connection requires recovery" {
		t.Fatalf("err=%v", err)
	}
}

func TestWSConnectedLifecycle(t *testing.T) {
	conn := newFakeWS()
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":2,"result":[]}`)}
	client, err := NewWSClient(nil, WSConfig{
		URL: "wss://test.deribit.com/ws/api/v2", Channels: []string{"book.ETH_BTC.none.50.100ms"},
		Dial:         func(context.Context, string) (WSConnection, error) { return conn, nil },
		ReconnectMin: time.Millisecond, ReconnectMax: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, func(context.Context, WSNotification) error { return nil }) }()
	waitForWS(t, func() bool { return len(conn.methods()) >= 2 })
	if !client.Connected() {
		t.Fatal("Connected() = false while Deribit connection is serving")
	}
	metrics := client.Metrics()
	if metrics.LastReceiveAt.IsZero() || !metrics.LastEventAt.IsZero() {
		t.Fatalf("Deribit stream times = %+v", metrics)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not terminate")
	}
	if client.Connected() {
		t.Fatal("Connected() = true after Deribit connection stopped")
	}
}

func waitForWS(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func TestPrivateWSUsesTokenAndPrivateSubscribe(t *testing.T) {
	conn := newFakeWS()
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":2,"result":{"scope":"connection","enabled":false}}`)}
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":3,"result":[]}`)}
	conn.reads <- fakeWSRead{err: io.EOF}
	httpClient := &Client{token: "token", tokenExpiresAt: time.Now().Add(time.Hour), now: time.Now}
	client, _ := NewWSClient(httpClient, WSConfig{URL: "wss://test.deribit.com/ws/api/v2", Channels: []string{"user.changes.any.any.raw"}, Private: true, Dial: func(context.Context, string) (WSConnection, error) { return conn, nil }})
	_ = client.runConnection(context.Background(), conn, 1, func(context.Context, WSNotification) error { return nil })
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.writes) != 3 || conn.writes[1].Method != "private/get_cancel_on_disconnect" || conn.writes[2].Method != "private/subscribe" {
		t.Fatalf("writes=%+v", conn.writes)
	}
	params := conn.writes[2].Params.(map[string]any)
	if params["access_token"] != "token" {
		t.Fatal("private subscription token absent")
	}
	metrics := client.Metrics()
	if !metrics.CODQueried || metrics.CODScope != "connection" || metrics.CODEnabled {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestPrivateWSCODHandlesReorderedResponsesAndEarlyEvent(t *testing.T) {
	conn := newFakeWS()
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","method":"subscription","params":{"channel":"user.changes.any.any.raw","data":{"orders":[{"order_id":"early"}]}}}`)}
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":4,"result":[]}`)}
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":3,"result":{"scope":"connection","enabled":true}}`)}
	conn.reads <- fakeWSRead{payload: wsPayload(`{"jsonrpc":"2.0","id":2,"result":"ok"}`)}

	httpClient := &Client{token: "token", tokenExpiresAt: time.Now().Add(time.Hour), now: time.Now}
	stream, err := NewWSClient(httpClient, WSConfig{
		URL:                 "wss://test.deribit.com/ws/api/v2",
		Channels:            []string{"user.changes.any.any.raw"},
		Private:             true,
		EnableConnectionCOD: true,
		Dial:                func(context.Context, string) (WSConnection, error) { return conn, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- stream.Run(ctx, func(_ context.Context, event WSNotification) error {
			if event.Channel != "user.changes.any.any.raw" {
				t.Errorf("channel=%q", event.Channel)
			}
			cancel()
			return nil
		})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not deliver buffered early event")
	}
	conn.mu.Lock()
	methods := make([]string, len(conn.writes))
	for index, write := range conn.writes {
		methods[index] = write.Method
	}
	conn.mu.Unlock()
	want := []string{"public/set_heartbeat", "private/enable_cancel_on_disconnect", "private/get_cancel_on_disconnect", "private/subscribe"}
	if !slices.Equal(methods, want) {
		t.Fatalf("methods=%v, want %v", methods, want)
	}
	metrics := stream.Metrics()
	if metrics.Ready != 1 || !metrics.CODQueried || !metrics.CODEnabled || metrics.Notifications != 1 {
		t.Fatalf("metrics=%+v", metrics)
	}
}
