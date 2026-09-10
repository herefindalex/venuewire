package ws

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type PrivateSink func(context.Context, PrivateEvent) error

type PrivateAuthError struct{ Message string }

func (e *PrivateAuthError) Error() string {
	return "private WebSocket authentication rejected: " + e.Message
}

type PrivateConfig struct {
	URL            string
	APIKey         string
	APISecret      string
	Topics         []string
	QueueSize      int
	PingInterval   time.Duration
	StaleAfter     time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Dial           DialFunc
	Now            func() time.Time
	OnReconnect    func(context.Context) error
}

type PrivateClient struct {
	config       PrivateConfig
	connected    atomic.Bool
	reconnects   atomic.Uint64
	messages     atomic.Uint64
	queueDepth   atomic.Int64
	sequenceRuns atomic.Uint64
}

func NewPrivateClient(config PrivateConfig) *PrivateClient {
	publicDefaults := NewClient(Config{URL: config.URL, QueueSize: config.QueueSize, PingInterval: config.PingInterval, StaleAfter: config.StaleAfter, InitialBackoff: config.InitialBackoff, MaxBackoff: config.MaxBackoff, Dial: config.Dial, Now: config.Now}).config
	config.QueueSize = publicDefaults.QueueSize
	config.PingInterval = publicDefaults.PingInterval
	config.StaleAfter = publicDefaults.StaleAfter
	config.InitialBackoff = publicDefaults.InitialBackoff
	config.MaxBackoff = publicDefaults.MaxBackoff
	config.Dial = publicDefaults.Dial
	config.Now = publicDefaults.Now
	if len(config.Topics) == 0 {
		config.Topics = []string{"order.linear", "execution.linear", "position.linear"}
	}
	return &PrivateClient{config: config}
}

func PrivateAuthSignature(secret string, expires int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(fmt.Sprintf("GET/realtime%d", expires)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (c *PrivateClient) Run(ctx context.Context, sink PrivateSink) error {
	if c.config.APIKey == "" || c.config.APISecret == "" {
		return errors.New("private WebSocket API credentials are required")
	}
	if sink == nil {
		return errors.New("private WebSocket sink is nil")
	}
	backoff := c.config.InitialBackoff
	hadSession := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		conn, err := c.config.Dial(ctx, c.config.URL)
		if err == nil {
			err = c.authenticate(conn)
		}
		if err == nil {
			err = conn.WriteJSON(map[string]any{"op": "subscribe", "args": c.config.Topics})
		}
		if err == nil && hadSession {
			c.reconnects.Add(1)
			if c.config.OnReconnect != nil {
				err = c.config.OnReconnect(ctx)
			}
		}
		if err == nil {
			hadSession = true
			backoff = c.config.InitialBackoff
			err = c.serve(ctx, conn, sink)
		} else if conn != nil {
			_ = conn.Close()
		}
		if ctx.Err() != nil {
			return nil
		}
		var authErr *PrivateAuthError
		if errors.As(err, &authErr) {
			return err
		}
		timer := time.NewTimer(jitter(backoff))
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

func (c *PrivateClient) authenticate(conn Connection) error {
	expires := c.config.Now().UTC().UnixMilli() + 10_000
	if err := conn.WriteJSON(map[string]any{"op": "auth", "args": []any{c.config.APIKey, expires, PrivateAuthSignature(c.config.APISecret, expires)}}); err != nil {
		return fmt.Errorf("write private WebSocket authentication: %w", err)
	}
	_ = conn.SetReadDeadline(c.config.Now().Add(c.config.StaleAfter))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read private WebSocket authentication: %w", err)
	}
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"ret_msg"`
		Op      string `json:"op"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return fmt.Errorf("decode private WebSocket authentication: %w", err)
	}
	if response.Op != "auth" || !response.Success {
		return &PrivateAuthError{Message: response.Message}
	}
	return nil
}

func (c *PrivateClient) serve(parent context.Context, conn Connection, sink PrivateSink) error {
	c.connected.Store(true)
	defer c.connected.Store(false)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer conn.Close()
	_ = conn.SetReadDeadline(c.config.Now().Add(c.config.StaleAfter))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(c.config.Now().Add(c.config.StaleAfter)) })
	events := make(chan PrivateEvent, c.config.QueueSize)
	errorsOut := make(chan error, 3)
	var writeMu sync.Mutex
	var group sync.WaitGroup
	group.Add(3)
	go func() {
		defer group.Done()
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				reportError(errorsOut, fmt.Errorf("read private WebSocket: %w", err))
				return
			}
			received := c.config.Now().UTC()
			_ = conn.SetReadDeadline(received.Add(c.config.StaleAfter))
			decoded, err := DecodePrivate(payload, received)
			if err != nil {
				reportError(errorsOut, err)
				return
			}
			c.messages.Add(1)
			for _, event := range decoded {
				select {
				case events <- event:
					c.queueDepth.Add(1)
				case <-ctx.Done():
					return
				default:
					reportError(errorsOut, errors.New("private WebSocket queue full; reconnect and reconciliation required"))
					return
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
				c.queueDepth.Add(-1)
				if err := sink(ctx, event); err != nil {
					reportError(errorsOut, err)
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
				if err := conn.WriteJSON(map[string]string{"op": "ping"}); err != nil {
					writeMu.Unlock()
					reportError(errorsOut, err)
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

func (c *PrivateClient) Stats() (reconnects, messages uint64, queueDepth int64) {
	return c.reconnects.Load(), c.messages.Load(), c.queueDepth.Load()
}

func (c *PrivateClient) Connected() bool { return c.connected.Load() }
