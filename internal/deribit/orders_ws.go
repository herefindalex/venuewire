package deribit

import (
	"bytes"
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
	side = strings.ToLower(side)
	if side != "buy" && side != "sell" {
		return OrderResult{}, errors.New("side must be buy or sell")
	}
	var result OrderResult
	err := c.privateWriteWS(ctx, wsURL, "private/"+side, params, &result)
	return result, err
}

func (c *Client) EditWS(ctx context.Context, wsURL, orderID, amount, price string) (OrderResult, error) {
	params := map[string]any{"order_id": orderID, "amount": json.Number(amount)}
	if price != "" {
		params["price"] = json.Number(price)
	}
	var result OrderResult
	err := c.privateWriteWS(ctx, wsURL, "private/edit", params, &result)
	return result, err
}

func (c *Client) CancelWS(ctx context.Context, wsURL, orderID string) (Order, error) {
	var result Order
	err := c.privateWriteWS(ctx, wsURL, "private/cancel", map[string]string{"order_id": orderID}, &result)
	return result, err
}

func (c *Client) privateWriteWS(ctx context.Context, wsURL, method string, params any, result any) error {
	parsed, err := url.Parse(wsURL)
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" {
		return errors.New("invalid Deribit order WebSocket URL")
	}
	allowed := map[string]bool{"private/buy": true, "private/sell": true, "private/edit": true, "private/cancel": true}
	if !allowed[method] {
		return errors.New("unsupported Deribit WebSocket write method")
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return err
	}
	if err := c.acquire(ctx); err != nil {
		return err
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
		return fmt.Errorf("dial Deribit order WebSocket: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = conn.SetReadDeadline(deadline)

	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode %s parameters: %w", method, err)
	}
	requestParams := make(map[string]any)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&requestParams); err != nil {
		return fmt.Errorf("decode %s parameters: %w", method, err)
	}
	requestParams["access_token"] = token
	id := c.nextID.Add(1)
	if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: id, Method: method, Params: requestParams}); err != nil {
		return &OutcomeUnknownError{Method: method + " WebSocket", Cause: err}
	}

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return &OutcomeUnknownError{Method: method + " WebSocket", Cause: err}
		}
		var message wsEnvelope
		if err := json.Unmarshal(payload, &message); err != nil || message.JSONRPC != "2.0" {
			return &OutcomeUnknownError{Method: method + " WebSocket", Cause: errors.New("malformed JSON-RPC response")}
		}
		if message.Method == "heartbeat" && message.Params.Type == "test_request" {
			testID := c.nextID.Add(1)
			if err := conn.WriteJSON(request{JSONRPC: "2.0", ID: testID, Method: "public/test"}); err != nil {
				return &OutcomeUnknownError{Method: method + " WebSocket", Cause: err}
			}
			continue
		}
		if message.ID != id {
			continue
		}
		if message.Error != nil {
			message.Error.Data = nil
			return message.Error
		}
		if len(message.Result) == 0 || string(message.Result) == "null" {
			return &OutcomeUnknownError{Method: method, Cause: errors.New("missing WebSocket write result")}
		}
		if err := json.Unmarshal(message.Result, result); err != nil {
			return &OutcomeUnknownError{Method: method, Cause: errors.New("malformed WebSocket write result")}
		}
		return nil
	}
}
