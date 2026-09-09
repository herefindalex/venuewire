package deribit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type capturedRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      uint64         `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

func respond(t *testing.T, writer http.ResponseWriter, id uint64, result any, rpcErr any) {
	t.Helper()
	w := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		w["error"] = rpcErr
	} else {
		w["result"] = result
	}
	if err := json.NewEncoder(writer).Encode(w); err != nil {
		t.Error(err)
	}
}

func TestPublicCallValidatesEnvelopeAndUsesMonotonicIDs(t *testing.T) {
	var last atomic.Uint64
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			t.Error(err)
			return
		}
		if got.JSONRPC != "2.0" || got.Method != "public/get_time" {
			t.Errorf("bad request: %+v", got)
		}
		previous := last.Swap(got.ID)
		if got.ID <= previous {
			t.Errorf("non-monotonic id %d after %d", got.ID, previous)
		}
		respond(t, writer, got.ID, int64(1234), nil)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := client.ServerTime(context.Background())
		if err != nil || got != 1234 {
			t.Fatalf("time=%d err=%v", got, err)
		}
	}
}

func TestRPCBusinessAndMalformedErrorsAreTypedAndSanitized(t *testing.T) {
	secret := "do-not-leak-this-secret"
	for _, tc := range []struct {
		name      string
		handler   http.HandlerFunc
		wantTyped bool
	}{
		{"business", func(writer http.ResponseWriter, req *http.Request) {
			var got capturedRequest
			_ = json.NewDecoder(req.Body).Decode(&got)
			respond(t, writer, got.ID, nil, map[string]any{"code": 10001, "message": "bad request", "data": secret})
		}, true},
		{"malformed", func(writer http.ResponseWriter, req *http.Request) { _, _ = writer.Write([]byte(`not-json`)) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tc.handler)
			defer server.Close()
			client, _ := NewClient(server.URL, "key", secret, server.Client())
			_, err := client.ServerTime(context.Background())
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe error: %v", err)
			}
			_, typed := err.(*RPCError)
			if typed != tc.wantTyped {
				t.Fatalf("typed=%v err=%T", typed, err)
			}
		})
	}
}

func TestRPCErrorRemainsTypedOnHTTP400(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		writer.WriteHeader(http.StatusBadRequest)
		respond(t, writer, got.ID, nil, map[string]any{"code": 10001, "message": "invalid params"})
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "", "", server.Client())
	_, err := client.ServerTime(context.Background())
	if _, ok := err.(*RPCError); !ok {
		t.Fatalf("error type=%T, want *RPCError (%v)", err, err)
	}
}

func TestTokenIsCachedAcrossConcurrentPrivateReads(t *testing.T) {
	var authCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		switch got.Method {
		case "public/auth":
			authCalls.Add(1)
			respond(t, writer, got.ID, map[string]any{"access_token": "token", "expires_in": 300}, nil)
		case "private/get_account_summary":
			if req.Header.Get("Authorization") != "Bearer token" {
				t.Errorf("missing bearer token")
			}
			respond(t, writer, got.ID, map[string]any{"currency": "BTC", "balance": 100}, nil)
		default:
			t.Errorf("unexpected method %s", got.Method)
		}
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "key", "secret", server.Client())
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := client.AccountSummary(context.Background(), "BTC"); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if got := authCalls.Load(); got != 1 {
		t.Fatalf("auth calls=%d, want 1", got)
	}
}

func TestPrivateReadRefreshesOnceAfterAuthError(t *testing.T) {
	var authCalls, readCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		if got.Method == "public/auth" {
			n := authCalls.Add(1)
			respond(t, writer, got.ID, map[string]any{"access_token": fmt.Sprintf("token-%d", n), "expires_in": 300}, nil)
			return
		}
		if readCalls.Add(1) == 1 {
			respond(t, writer, got.ID, nil, map[string]any{"code": 13009, "message": "unauthorized"})
			return
		}
		respond(t, writer, got.ID, []any{}, nil)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "key", "secret", server.Client())
	if _, err := client.Positions(context.Background(), "BTC", "future"); err != nil {
		t.Fatal(err)
	}
	if authCalls.Load() != 2 || readCalls.Load() != 2 {
		t.Fatalf("auth=%d read=%d", authCalls.Load(), readCalls.Load())
	}
}

func TestClientRejectsInvalidMethodBoundaryAndEnvelopeID(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		respond(t, writer, got.ID+1, true, nil)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "", "", server.Client())
	if err := client.Public(context.Background(), "private/buy", nil, nil); err == nil {
		t.Fatal("private method accepted as public")
	}
	if _, err := client.ServerTime(context.Background()); err == nil {
		t.Fatal("mismatched response ID accepted")
	}
}

func TestAccountSummariesUsesAccountScopedAggregateResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		if got.Method == "public/auth" {
			respond(t, writer, got.ID, map[string]any{"access_token": "token", "expires_in": 300}, nil)
			return
		}
		if got.Method != "private/get_account_summaries" {
			t.Errorf("unexpected method %s", got.Method)
		}
		respond(t, writer, got.ID, map[string]any{"summaries": []any{
			map[string]any{"currency": "BTC", "balance": 100},
			map[string]any{"currency": "USDT", "balance": 100000},
		}}, nil)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "key", "secret", server.Client())
	got, err := client.AccountSummaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Currency != "BTC" || got[1].Currency != "USDT" {
		t.Fatalf("unexpected summaries: %+v", got)
	}
}

func TestPrivateWriteDoesNotRetryAmbiguousTransportFailure(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		if got.Method == "public/auth" {
			respond(t, writer, got.ID, map[string]any{"access_token": "token", "expires_in": 300}, nil)
			return
		}
		writes.Add(1)
		_, _ = writer.Write([]byte(`not-json`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "key", "secret", server.Client())
	_, err := client.Place(context.Background(), "buy", PlaceParams{InstrumentName: "BTC-PERPETUAL", Amount: "10", Type: "limit", Price: "80000", Label: "one"})
	var unknown *OutcomeUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("err=%T %v", err, err)
	}
	if writes.Load() != 1 {
		t.Fatalf("write attempts=%d", writes.Load())
	}
}

func TestTradesByOrderAcceptsDirectArray(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		if got.Method == "public/auth" {
			respond(t, writer, got.ID, map[string]any{"access_token": "token", "expires_in": 300}, nil)
			return
		}
		respond(t, writer, got.ID, []any{map[string]any{"trade_id": "trade-1", "order_id": "order-1", "fee": 0.0001}}, nil)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "key", "secret", server.Client())
	trades, err := client.TradesByOrder(context.Background(), "order-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 || trades[0].TradeID != "trade-1" {
		t.Fatalf("trades=%+v", trades)
	}
}

func TestInstrumentMetadataIsCached(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		calls.Add(1)
		respond(t, writer, got.ID, map[string]any{"instrument_name": "BTC-PERPETUAL", "kind": "future", "is_active": true, "tick_size": 0.5, "min_trade_amount": 10}, nil)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "", "", server.Client())
	for range 2 {
		instrument, err := client.Instrument(context.Background(), "BTC-PERPETUAL")
		if err != nil || instrument.InstrumentName != "BTC-PERPETUAL" {
			t.Fatalf("instrument=%+v err=%v", instrument, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("metadata calls=%d", calls.Load())
	}
}

func TestRequestLimiterHonorsContextCancellation(t *testing.T) {
	client, _ := NewClient("https://test.deribit.com/api/v2", "", "", nil)
	client.requestSlots = make(chan struct{}, 1)
	client.requestSlots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ServerTime(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestCancelOnDisconnectReadsRequestedScope(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var got capturedRequest
		_ = json.NewDecoder(req.Body).Decode(&got)
		switch got.Method {
		case "public/auth":
			respond(t, writer, got.ID, map[string]any{"access_token": "token", "expires_in": 300}, nil)
		case "private/get_cancel_on_disconnect":
			if got.Params["scope"] != "account" {
				t.Errorf("scope=%v", got.Params["scope"])
			}
			respond(t, writer, got.ID, map[string]any{"scope": "account", "enabled": false}, nil)
		default:
			t.Errorf("unexpected method %s", got.Method)
		}
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "key", "secret", server.Client())
	status, err := client.CancelOnDisconnect(context.Background(), "account")
	if err != nil {
		t.Fatal(err)
	}
	if status.Scope != "account" || status.Enabled {
		t.Fatalf("status=%+v", status)
	}
}

func FuzzRPCEnvelopeDecode(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"result":{"value":0.00000001}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":10028,"message":"too_many_requests"}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 4<<20 {
			t.Skip()
		}
		var decoded envelope
		_ = decodeRPCEnvelope(raw, &decoded)
	})
}
