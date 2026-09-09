package ws

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReconnectResubscribesAfterEOF(t *testing.T) {
	first := newFakeConnection()
	first.reads <- readResult{err: errors.New("EOF")}
	second := newFakeConnection()
	second.reads <- readResult{payload: tradeFixture("trade-after-reconnect")}
	connections := make(chan *fakeConnection, 2)
	connections <- first
	connections <- second
	client := NewClient(Config{
		URL: "fixture", Topics: []string{"publicTrade.BTCUSDT"}, QueueSize: 4,
		InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		PingInterval: time.Hour, StaleAfter: time.Hour,
		Dial: func(context.Context, string) (Connection, error) { return <-connections, nil },
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, func(_ context.Context, event Event) error {
			if event.Trade != nil && event.Trade.ID == "trade-after-reconnect" {
				cancel()
			}
			return nil
		})
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(first.subscriptions()) != 1 || len(second.subscriptions()) != 2 {
		t.Fatalf("subscriptions: first=%v second=%v", first.subscriptions(), second.subscriptions())
	}
	stats := client.Stats()
	if stats.Reconnects != 1 || stats.Subscriptions != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if second.deadlineCount() < 2 {
		t.Fatalf("active message did not refresh stale deadline: calls=%d", second.deadlineCount())
	}
}

func TestDialNetworkErrorRetriesAndEventuallyStreams(t *testing.T) {
	connection := newFakeConnection()
	connection.reads <- readResult{payload: tradeFixture("after-dial-error")}
	var attempts atomic.Int64
	client := NewClient(Config{URL: "fixture", Topics: []string{"publicTrade.BTCUSDT"}, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, PingInterval: time.Hour, StaleAfter: time.Hour, Dial: func(context.Context, string) (Connection, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("network unreachable")
		}
		return connection, nil
	}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Run(ctx, func(_ context.Context, event Event) error {
		if event.Trade != nil {
			cancel()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("dial attempts=%d", attempts.Load())
	}
}

func TestContextCancellationClosesConnection(t *testing.T) {
	connection := newFakeConnection()
	client := NewClient(Config{URL: "fixture", Topics: []string{"publicTrade.BTCUSDT"}, Dial: func(context.Context, string) (Connection, error) { return connection, nil }, PingInterval: time.Hour, StaleAfter: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, func(context.Context, Event) error { return nil }) }()
	waitFor(t, func() bool { return len(connection.subscriptions()) == 1 })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not terminate")
	}
	if !connection.isClosed() {
		t.Fatal("connection was not closed")
	}
}

func TestBoundedQueueDropsPublicEvents(t *testing.T) {
	connection := newFakeConnection()
	client := NewClient(Config{URL: "fixture", Topics: []string{"publicTrade.BTCUSDT"}, QueueSize: 1, Dial: func(context.Context, string) (Connection, error) { return connection, nil }, PingInterval: time.Hour, StaleAfter: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	sinkEntered := make(chan struct{}, 1)
	releaseSink := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, func(context.Context, Event) error {
			sinkEntered <- struct{}{}
			<-releaseSink
			return nil
		})
	}()
	connection.reads <- readResult{payload: tradeFixture("first")}
	<-sinkEntered
	for i := 0; i < 20; i++ {
		connection.reads <- readResult{payload: tradeFixture("overflow")}
	}
	waitFor(t, func() bool { return client.Stats().Dropped > 0 })
	cancel()
	close(releaseSink)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if client.Stats().Dropped == 0 {
		t.Fatal("bounded queue did not report drops")
	}
}

func TestStaleReadFailureReconnects(t *testing.T) {
	first := newFakeConnection()
	first.reads <- readResult{err: errors.New("read deadline exceeded")}
	second := newFakeConnection()
	connections := make(chan *fakeConnection, 2)
	connections <- first
	connections <- second
	client := NewClient(Config{URL: "fixture", Topics: []string{"publicTrade.BTCUSDT"}, Dial: func(context.Context, string) (Connection, error) { return <-connections, nil }, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, PingInterval: time.Hour, StaleAfter: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, func(context.Context, Event) error { return nil }) }()
	waitFor(t, func() bool { return len(second.subscriptions()) == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if first.deadline().IsZero() {
		t.Fatal("stale read deadline was not configured")
	}
}

type readResult struct {
	payload []byte
	err     error
}

type fakeConnection struct {
	reads         chan readResult
	mu            sync.Mutex
	writes        []any
	closed        bool
	readDeadline  time.Time
	deadlineCalls int
	pong          func(string) error
}

func newFakeConnection() *fakeConnection { return &fakeConnection{reads: make(chan readResult, 64)} }

func (c *fakeConnection) ReadMessage() (int, []byte, error) {
	result, ok := <-c.reads
	if !ok {
		return 0, nil, errors.New("closed")
	}
	return 1, result.payload, result.err
}
func (c *fakeConnection) WriteJSON(value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, value)
	return nil
}
func (c *fakeConnection) WriteControl(int, []byte, time.Time) error { return nil }
func (c *fakeConnection) SetReadDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.readDeadline = deadline
	c.deadlineCalls++
	c.mu.Unlock()
	return nil
}
func (c *fakeConnection) SetPongHandler(handler func(string) error) {
	c.mu.Lock()
	c.pong = handler
	c.mu.Unlock()
}
func (c *fakeConnection) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.reads)
	}
	c.mu.Unlock()
	return nil
}
func (c *fakeConnection) subscriptions() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]any(nil), c.writes...)
}
func (c *fakeConnection) isClosed() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.closed }
func (c *fakeConnection) deadline() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readDeadline
}
func (c *fakeConnection) deadlineCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deadlineCalls
}

func tradeFixture(id string) []byte {
	return []byte(`{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1700000000100,"data":[{"T":1700000000125,"s":"BTCUSDT","S":"Buy","v":"0.001","p":"42000.10","i":"` + id + `"}]}`)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met")
}
