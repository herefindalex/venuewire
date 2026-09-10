package ws

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Connection interface {
	ReadMessage() (int, []byte, error)
	WriteJSON(any) error
	WriteControl(int, []byte, time.Time) error
	SetReadDeadline(time.Time) error
	SetPongHandler(func(string) error)
	Close() error
}

type DialFunc func(context.Context, string) (Connection, error)
type Sink func(context.Context, Event) error

type Config struct {
	URL            string
	Topics         []string
	QueueSize      int
	PingInterval   time.Duration
	StaleAfter     time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Dial           DialFunc
	Now            func() time.Time
}

type Stats struct {
	Reconnects    uint64
	Messages      uint64
	Dropped       uint64
	DecodeErrors  uint64
	Subscriptions uint64
}

type Client struct {
	config        Config
	connected     atomic.Bool
	reconnects    atomic.Uint64
	messages      atomic.Uint64
	dropped       atomic.Uint64
	decodeErrors  atomic.Uint64
	subscriptions atomic.Uint64
}

func NewClient(config Config) *Client {
	if config.QueueSize <= 0 {
		config.QueueSize = 256
	}
	if config.PingInterval <= 0 {
		config.PingInterval = 20 * time.Second
	}
	if config.StaleAfter <= 0 {
		config.StaleAfter = 45 * time.Second
	}
	if config.InitialBackoff <= 0 {
		config.InitialBackoff = 250 * time.Millisecond
	}
	if config.MaxBackoff <= 0 {
		config.MaxBackoff = 15 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Dial == nil {
		config.Dial = func(ctx context.Context, rawURL string) (Connection, error) {
			conn, response, err := websocket.DefaultDialer.DialContext(ctx, rawURL, http.Header{})
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return conn, err
		}
	}
	return &Client{config: config}
}

func (c *Client) Stats() Stats {
	return Stats{Reconnects: c.reconnects.Load(), Messages: c.messages.Load(), Dropped: c.dropped.Load(), DecodeErrors: c.decodeErrors.Load(), Subscriptions: c.subscriptions.Load()}
}

func (c *Client) Connected() bool { return c.connected.Load() }

func (c *Client) Run(ctx context.Context, sink Sink) error {
	if sink == nil {
		return errors.New("WebSocket sink is nil")
	}
	backoff := c.config.InitialBackoff
	connected := false
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		conn, err := c.config.Dial(ctx, c.config.URL)
		if err == nil {
			if connected {
				c.reconnects.Add(1)
			}
			connected = true
			backoff = c.config.InitialBackoff
			err = c.serve(ctx, conn, sink)
		}
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return nil
		}
		wait := jitter(backoff)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff *= 2
		if backoff > c.config.MaxBackoff {
			backoff = c.config.MaxBackoff
		}
	}
}

func (c *Client) serve(parent context.Context, conn Connection, sink Sink) error {
	c.connected.Store(true)
	defer c.connected.Store(false)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"op": "subscribe", "args": c.config.Topics}); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	c.subscriptions.Add(1)
	_ = conn.SetReadDeadline(c.config.Now().Add(c.config.StaleAfter))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(c.config.Now().Add(c.config.StaleAfter))
	})

	events := make(chan Event, c.config.QueueSize)
	errorsOut := make(chan error, 3)
	var writeMu sync.Mutex
	var group sync.WaitGroup
	group.Add(3)
	go func() {
		defer group.Done()
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				reportError(errorsOut, fmt.Errorf("read WebSocket: %w", err))
				return
			}
			received := c.config.Now().UTC()
			_ = conn.SetReadDeadline(received.Add(c.config.StaleAfter))
			decoded, err := DecodePublic(payload, received)
			if err != nil {
				c.decodeErrors.Add(1)
				continue
			}
			c.messages.Add(1)
			for _, event := range decoded {
				select {
				case events <- event:
				case <-ctx.Done():
					return
				default:
					c.dropped.Add(1)
				}
			}
		}
	}()
	go func() {
		defer group.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-events:
				if err := sink(ctx, event); err != nil {
					reportError(errorsOut, fmt.Errorf("process WebSocket event: %w", err))
					return
				}
			}
		}
	}()
	go func() {
		defer group.Done()
		ticker := time.NewTicker(c.config.PingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				writeMu.Lock()
				if err := conn.WriteControl(websocket.PingMessage, nil, c.config.Now().Add(5*time.Second)); err != nil {
					writeMu.Unlock()
					reportError(errorsOut, fmt.Errorf("ping WebSocket: %w", err))
					return
				}
				writeMu.Unlock()
			}
		}
	}()

	var err error
	graceful := false
	select {
	case <-parent.Done():
		graceful = true
	case err = <-errorsOut:
	}
	cancel()
	if graceful {
		writeMu.Lock()
		_ = conn.WriteJSON(map[string]any{"op": "unsubscribe", "args": c.config.Topics})
		writeMu.Unlock()
	}
	_ = conn.Close()
	group.Wait()
	return err
}

func reportError(destination chan<- error, err error) {
	select {
	case destination <- err:
	default:
	}
}

func jitter(base time.Duration) time.Duration {
	if base <= 1 {
		return base
	}
	return base/2 + time.Duration(rand.Int64N(int64(base/2)))
}
