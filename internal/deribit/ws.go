package deribit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type WSConnection interface {
	ReadMessage() (int, []byte, error)
	WriteJSON(any) error
	SetReadDeadline(time.Time) error
	SetPongHandler(func(string) error)
	Close() error
}

type WSDialFunc func(context.Context, string) (WSConnection, error)

type WSNotification struct {
	Channel    string
	Data       json.RawMessage
	ReceivedAt time.Time
	Generation uint64
}

type WSConfig struct {
	URL                 string
	Channels            []string
	Private             bool
	EnableConnectionCOD bool
	QueueSize           int
	HeartbeatInterval   time.Duration
	StaleAfter          time.Duration
	ReconnectMin        time.Duration
	ReconnectMax        time.Duration
	Dial                WSDialFunc
	Now                 func() time.Time
	OnReady             func(context.Context, uint64) error
}

type WSMetrics struct {
	Connections   uint64
	Reconnects    uint64
	Messages      uint64
	Notifications uint64
	TestRequests  uint64
	QueueDepth    int64
	LastError     string
	Ready         uint64
	CODQueried    bool
	CODScope      string
	CODEnabled    bool
}

type WSClient struct {
	httpClient    *Client
	config        WSConfig
	nextID        atomic.Uint64
	connections   atomic.Uint64
	reconnects    atomic.Uint64
	messages      atomic.Uint64
	notifications atomic.Uint64
	testRequests  atomic.Uint64
	queueDepth    atomic.Int64
	errorMu       sync.Mutex
	lastError     string
	codQueried    bool
	codScope      string
	codEnabled    bool
	ready         atomic.Uint64
}

type wsEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	Params  struct {
		Channel string          `json:"channel,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
		Type    string          `json:"type,omitempty"`
	} `json:"params,omitempty"`
}

func NewWSClient(httpClient *Client, config WSConfig) (*WSClient, error) {
	parsed, err := url.Parse(config.URL)
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" {
		return nil, errors.New("invalid Deribit WSS URL")
	}
	if len(config.Channels) == 0 {
		return nil, errors.New("at least one Deribit channel is required")
	}
	if config.Private && httpClient == nil {
		return nil, errors.New("private Deribit WebSocket requires HTTP auth client")
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 128
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = 10 * time.Second
	}
	if config.StaleAfter <= 0 {
		config.StaleAfter = 3 * config.HeartbeatInterval
	}
	if config.ReconnectMin <= 0 {
		config.ReconnectMin = 100 * time.Millisecond
	}
	if config.ReconnectMax <= 0 {
		config.ReconnectMax = 5 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Dial == nil {
		config.Dial = func(ctx context.Context, rawURL string) (WSConnection, error) {
			conn, response, err := websocket.DefaultDialer.DialContext(ctx, rawURL, http.Header{})
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return conn, err
		}
	}
	return &WSClient{httpClient: httpClient, config: config}, nil
}

func (c *WSClient) Run(ctx context.Context, sink func(context.Context, WSNotification) error) error {
	if sink == nil {
		return errors.New("Deribit WebSocket sink is required")
	}
	backoff := c.config.ReconnectMin
	var generation uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := c.config.Dial(ctx, c.config.URL)
		if err != nil {
			if err := waitContext(ctx, backoff); err != nil {
				return err
			}
			backoff = nextBackoff(backoff, c.config.ReconnectMax)
			continue
		}
		generation++
		c.connections.Add(1)
		if generation > 1 {
			c.reconnects.Add(1)
		}
		err = c.runConnection(ctx, conn, generation, sink)
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.errorMu.Lock()
		c.lastError = err.Error()
		c.errorMu.Unlock()
		if err := waitContext(ctx, backoff); err != nil {
			return err
		}
		backoff = nextBackoff(backoff, c.config.ReconnectMax)
	}
}

func (c *WSClient) runConnection(ctx context.Context, conn WSConnection, generation uint64, sink func(context.Context, WSNotification) error) error {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	if err := conn.SetReadDeadline(c.config.Now().Add(c.config.StaleAfter)); err != nil {
		return err
	}
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(c.config.Now().Add(c.config.StaleAfter)) })
	heartbeatID := c.nextID.Add(1)
	if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: heartbeatID, Method: "public/set_heartbeat", Params: map[string]any{"interval": int(c.config.HeartbeatInterval.Seconds())}}); err != nil {
		return err
	}
	params := map[string]any{"channels": c.config.Channels}
	method := "public/subscribe"
	var codEnableID, codQueryID uint64
	if c.config.Private {
		token, err := c.httpClient.accessToken(ctx, false)
		if err != nil {
			return err
		}
		params["access_token"] = token
		method = "private/subscribe"
		if c.config.EnableConnectionCOD {
			codEnableID = c.nextID.Add(1)
			if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: codEnableID, Method: "private/enable_cancel_on_disconnect", Params: map[string]any{"access_token": token, "scope": "connection"}}); err != nil {
				return err
			}
		}
		codQueryID = c.nextID.Add(1)
		if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: codQueryID, Method: "private/get_cancel_on_disconnect", Params: map[string]any{"access_token": token, "scope": "connection"}}); err != nil {
			return err
		}
	}
	subscribeID := c.nextID.Add(1)
	if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: subscribeID, Method: method, Params: params}); err != nil {
		return err
	}

	events := make(chan WSNotification, c.config.QueueSize)
	processorErrors := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				processorErrors <- ctx.Err()
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				c.queueDepth.Add(-1)
				if err := sink(ctx, event); err != nil {
					processorErrors <- err
					return
				}
			}
		}
	}()
	defer close(events)
	ready := false
	subscribed := false
	codQueryDone := !c.config.Private
	codEnableDone := !c.config.EnableConnectionCOD
	pendingNotifications := make([]WSNotification, 0, c.config.QueueSize)
	enqueue := func(event WSNotification) error {
		select {
		case events <- event:
			c.queueDepth.Add(1)
			c.notifications.Add(1)
			return nil
		default:
			return errors.New("Deribit WebSocket event queue full; connection requires recovery")
		}
	}
	becomeReady := func() error {
		if ready || !subscribed || !codQueryDone || !codEnableDone {
			return nil
		}
		if c.config.OnReady != nil {
			if err := c.config.OnReady(ctx, generation); err != nil {
				return fmt.Errorf("Deribit recovery callback: %w", err)
			}
		}
		ready = true
		c.ready.Add(1)
		for _, event := range pendingNotifications {
			if err := enqueue(event); err != nil {
				return err
			}
		}
		pendingNotifications = nil
		return nil
	}
	for {
		select {
		case err := <-processorErrors:
			return err
		default:
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		c.messages.Add(1)
		_ = conn.SetReadDeadline(c.config.Now().Add(c.config.StaleAfter))
		var message wsEnvelope
		if err := json.Unmarshal(payload, &message); err != nil {
			return errors.New("malformed Deribit WebSocket JSON-RPC message")
		}
		if message.JSONRPC != "2.0" {
			return errors.New("invalid Deribit WebSocket JSON-RPC version")
		}
		if message.Error != nil {
			message.Error.Data = nil
			return message.Error
		}
		if message.ID != 0 {
			handled := true
			switch message.ID {
			case subscribeID:
				subscribed = true
			case codEnableID:
				if codEnableID == 0 {
					handled = false
					break
				}
				var result string
				if err := json.Unmarshal(message.Result, &result); err != nil || result != "ok" {
					return errors.New("Deribit connection COD enable returned an invalid acknowledgement")
				}
				codEnableDone = true
			case codQueryID:
				if codQueryID == 0 {
					handled = false
					break
				}
				var status CancelOnDisconnect
				if err := json.Unmarshal(message.Result, &status); err != nil || status.Scope != "connection" {
					return errors.New("Deribit connection COD query returned an invalid result")
				}
				c.errorMu.Lock()
				c.codQueried = true
				c.codScope = status.Scope
				c.codEnabled = status.Enabled
				c.errorMu.Unlock()
				codQueryDone = true
			default:
				handled = false
			}
			if handled {
				if err := becomeReady(); err != nil {
					return err
				}
				continue
			}
		}
		if message.Method == "heartbeat" && message.Params.Type == "test_request" {
			c.testRequests.Add(1)
			id := c.nextID.Add(1)
			if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: id, Method: "public/test"}); err != nil {
				return err
			}
			continue
		}
		if message.Method != "subscription" {
			continue
		}
		event := WSNotification{Channel: message.Params.Channel, Data: message.Params.Data, ReceivedAt: c.config.Now().UTC(), Generation: generation}
		if !ready {
			if len(pendingNotifications) >= c.config.QueueSize {
				return errors.New("Deribit WebSocket recovery buffer full before connection became ready")
			}
			pendingNotifications = append(pendingNotifications, event)
			continue
		}
		if err := enqueue(event); err != nil {
			return err
		}
	}
}

func (c *WSClient) Metrics() WSMetrics {
	c.errorMu.Lock()
	lastError := c.lastError
	codQueried := c.codQueried
	codScope := c.codScope
	codEnabled := c.codEnabled
	c.errorMu.Unlock()
	return WSMetrics{Connections: c.connections.Load(), Reconnects: c.reconnects.Load(), Messages: c.messages.Load(), Notifications: c.notifications.Load(), TestRequests: c.testRequests.Load(), QueueDepth: c.queueDepth.Load(), LastError: lastError, Ready: c.ready.Load(), CODQueried: codQueried, CODScope: codScope, CODEnabled: codEnabled}
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	next := current * 2
	if next > maximum {
		return maximum
	}
	return next
}
