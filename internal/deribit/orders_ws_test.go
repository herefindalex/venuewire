package deribit

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestPlaceWSUsesPrivateRPCAndReturnsResult(t *testing.T) {
	conn := newFakeWS()
	conn.reads <- fakeWSRead{payload: []byte(`{"jsonrpc":"2.0","id":1,"result":{"order":{"order_id":"one","label":"label","order_state":"open"},"trades":[]}}`)}
	client := &Client{token: "token", tokenExpiresAt: time.Now().Add(time.Hour), now: time.Now, orderWSDial: func(context.Context, string) (WSConnection, error) { return conn, nil }}
	result, err := client.PlaceWS(context.Background(), "wss://test.deribit.com/ws/api/v2", "buy", PlaceParams{InstrumentName: "BTC-PERPETUAL", Amount: "10", Type: "limit", Price: "78000", Label: "label"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Order.OrderID != "one" {
		t.Fatalf("result=%+v", result)
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.writes) != 1 || conn.writes[0].Method != "private/buy" {
		t.Fatalf("writes=%+v", conn.writes)
	}
	params := conn.writes[0].Params.(map[string]any)
	if params["access_token"] != "token" {
		t.Fatal("access token missing")
	}
}

func TestPlaceWSClassifiesPostWriteDisconnectUnknown(t *testing.T) {
	conn := newFakeWS()
	conn.reads <- fakeWSRead{err: io.EOF}
	client := &Client{token: "token", tokenExpiresAt: time.Now().Add(time.Hour), now: time.Now, orderWSDial: func(context.Context, string) (WSConnection, error) { return conn, nil }}
	_, err := client.PlaceWS(context.Background(), "wss://test.deribit.com/ws/api/v2", "buy", PlaceParams{InstrumentName: "BTC-PERPETUAL", Amount: "10", Type: "limit", Price: "78000", Label: "label"})
	var unknown *OutcomeUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("err=%T %v", err, err)
	}
}
