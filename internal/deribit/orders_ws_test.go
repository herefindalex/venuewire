package deribit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestPlaceParamsEncodeDecimalsAsJSONNumbers(t *testing.T) {
	raw, err := json.Marshal(PlaceParams{InstrumentName: "BTC-PERPETUAL", Amount: json.Number("10"), Type: "limit", Price: json.Number("78000.5")})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"amount":10`) || !strings.Contains(text, `"price":78000.5`) || strings.Contains(text, `"amount":"10"`) {
		t.Fatalf("wire JSON uses quoted decimal: %s", text)
	}
}

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

func TestEditAndCancelWSUseRequestedMethods(t *testing.T) {
	editConn := newFakeWS()
	editConn.reads <- fakeWSRead{payload: []byte(`{"jsonrpc":"2.0","id":1,"result":{"order":{"order_id":"one","order_state":"open","amount":10,"price":77999.5},"trades":[]}}`)}
	client := &Client{token: "token", tokenExpiresAt: time.Now().Add(time.Hour), now: time.Now, orderWSDial: func(context.Context, string) (WSConnection, error) { return editConn, nil }}
	edit, err := client.EditWS(context.Background(), "wss://test.deribit.com/ws/api/v2", "one", "10", "77999.5")
	if err != nil {
		t.Fatal(err)
	}
	if edit.Order.OrderID != "one" || edit.Order.Price.String() != "77999.5" {
		t.Fatalf("edit=%+v", edit)
	}
	editConn.mu.Lock()
	if len(editConn.writes) != 1 || editConn.writes[0].Method != "private/edit" {
		t.Fatalf("edit writes=%+v", editConn.writes)
	}
	editConn.mu.Unlock()

	cancelConn := newFakeWS()
	cancelConn.reads <- fakeWSRead{payload: []byte(`{"jsonrpc":"2.0","id":2,"result":{"order_id":"one","order_state":"cancelled"}}`)}
	client.orderWSDial = func(context.Context, string) (WSConnection, error) { return cancelConn, nil }
	cancelled, err := client.CancelWS(context.Background(), "wss://test.deribit.com/ws/api/v2", "one")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.OrderID != "one" || cancelled.OrderState != "cancelled" {
		t.Fatalf("cancel=%+v", cancelled)
	}
	cancelConn.mu.Lock()
	if len(cancelConn.writes) != 1 || cancelConn.writes[0].Method != "private/cancel" {
		t.Fatalf("cancel writes=%+v", cancelConn.writes)
	}
	cancelConn.mu.Unlock()
}
