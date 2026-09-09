package deribit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func (c *Client) PlaceWS(ctx context.Context, wsURL, side string, params PlaceParams) (OrderResult, error) {
	parsed, err := url.Parse(wsURL)
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" {
		return OrderResult{}, errors.New("invalid Deribit order WebSocket URL")
	}
	side = strings.ToLower(side)
	if side != "buy" && side != "sell" {
		return OrderResult{}, errors.New("side must be buy or sell")
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return OrderResult{}, err
	}
	if err := c.acquire(ctx); err != nil {
		return OrderResult{}, err
	}
	defer c.release()
	dial := c.orderWSDial
	if dial == nil {
		dial = func(ctx context.Context, rawURL string) (WSConnection, error) {
			conn, response, err := websocket.DefaultDialer.DialContext(ctx, rawURL, http.Header{})
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return conn, err
		}
	}
	conn, err := dial(ctx, wsURL)
	if err != nil {
		return OrderResult{}, fmt.Errorf("dial Deribit order WebSocket: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = conn.SetReadDeadline(deadline)
	raw, err := json.Marshal(params)
	if err != nil {
		return OrderResult{}, err
	}
	var requestParams map[string]any
	if err := json.Unmarshal(raw, &requestParams); err != nil {
		return OrderResult{}, err
	}
	requestParams["access_token"] = token
	id := c.nextID.Add(1)
	if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: id, Method: "private/" + side, Params: requestParams}); err != nil {
		return OrderResult{}, fmt.Errorf("write Deribit order WebSocket: %w", err)
	}
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return OrderResult{}, &OutcomeUnknownError{Method: "private/" + side, Cause: err}
		}
		var message wsEnvelope
		if err := json.Unmarshal(payload, &message); err != nil {
			return OrderResult{}, &OutcomeUnknownError{Method: "private/" + side, Cause: errors.New("malformed WebSocket response")}
		}
		if message.JSONRPC != "2.0" {
			return OrderResult{}, &OutcomeUnknownError{Method: "private/" + side, Cause: errors.New("invalid WebSocket JSON-RPC version")}
		}
		if message.Method == "heartbeat" && message.Params.Type == "test_request" {
			testID := c.nextID.Add(1)
			if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: testID, Method: "public/test"}); err != nil {
				return OrderResult{}, &OutcomeUnknownError{Method: "private/" + side, Cause: err}
			}
			continue
		}
		if message.ID != id {
			continue
		}
		if message.Error != nil {
			message.Error.Data = nil
			return OrderResult{}, message.Error
		}
		if len(message.Result) == 0 {
			return OrderResult{}, &OutcomeUnknownError{Method: "private/" + side, Cause: errors.New("missing WebSocket order result")}
		}
		var result OrderResult
		if err := json.Unmarshal(message.Result, &result); err != nil {
			return OrderResult{}, &OutcomeUnknownError{Method: "private/" + side, Cause: errors.New("malformed WebSocket order result")}
		}
		return result, nil
	}
}
