package rest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrivateGETUsesCanonicalQueryAndHeaders(t *testing.T) {
	fixed := time.UnixMilli(1700000000123)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.RawQuery, "category=linear&orderLinkId=client-1&symbol=BTCUSDT"; got != want {
			t.Errorf("query = %q, want %q", got, want)
		}
		assertHeader(t, r, "X-BAPI-API-KEY", "test-api-key")
		assertHeader(t, r, "X-BAPI-TIMESTAMP", "1700000000123")
		assertHeader(t, r, "X-BAPI-RECV-WINDOW", "5000")
		wantSign := Sign(fixed.UnixMilli(), "test-api-key", 5000, r.URL.RawQuery, "test-secret")
		assertHeader(t, r, "X-BAPI-SIGN", wantSign)
		w.Header().Set("X-Bapi-Limit", "50")
		w.Header().Set("X-Bapi-Limit-Status", "49")
		w.Header().Set("X-Bapi-Limit-Reset-Timestamp", "1700000000999")
		w.Header().Set("Traceid", "trace-fixture")
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"list":[]},"time":1700000000123}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key", "test-secret", WithClock(func() time.Time { return fixed }))
	orders, meta, err := client.Orders(context.Background(), "linear", "BTCUSDT", "", "client-1", nil)
	if err != nil {
		t.Fatalf("Orders() error: %v", err)
	}
	if len(orders) != 0 || meta.RateLimit.Limit != 50 || meta.RateLimit.Remaining != 49 || meta.RequestID != "trace-fixture" {
		t.Fatalf("unexpected response: orders=%+v meta=%+v", orders, meta)
	}
	if got := meta.RateLimit.ResetAt.UnixMilli(); got != 1700000000999 {
		t.Fatalf("reset = %d", got)
	}
}

func TestClientRejectsRedirectWithoutForwardingCredentials(t *testing.T) {
	var redirected atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/capture" {
			redirected.Add(1)
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"list":[]}}`)
			return
		}
		http.Redirect(writer, req, "/capture", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	client := NewClient(server.URL, "fixture-api-key", "fixture-secret")
	_, _, err := client.Orders(context.Background(), "linear", "BTCUSDT", "", "fixture-link", nil)
	if err == nil || !strings.Contains(err.Error(), "redirects are disabled") {
		t.Fatalf("expected explicit redirect refusal, got %v", err)
	}
	if redirected.Load() != 0 {
		t.Fatal("redirect target received a signed request")
	}
}

func TestExecutionsDecodeFeeDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/execution/list" {
			t.Errorf("path = %q, want /v5/execution/list", r.URL.Path)
		}
		if got, want := r.URL.Query().Get("orderLinkId"), "fee-fixture"; got != want {
			t.Errorf("orderLinkId = %q, want %q", got, want)
		}
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"list":[{"execId":"exec-1","orderId":"order-1","orderLinkId":"fee-fixture","symbol":"ETHUSDT","side":"Buy","execPrice":"2500","execQty":"1","execFee":"0.01","feeRate":"0.01","feeCurrency":"ETH","execTime":"1700000000123"}]}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key", "test-secret")
	executions, _, err := client.Executions(context.Background(), "spot", "ETHUSDT", "", "fee-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(executions) != 1 {
		t.Fatalf("execution count = %d, want 1", len(executions))
	}
	got := executions[0]
	if got.ExecFee != "0.01" || got.FeeRate != "0.01" || got.FeeCurrency != "ETH" {
		t.Fatalf("fee details = fee %q rate %q currency %q", got.ExecFee, got.FeeRate, got.FeeCurrency)
	}
}

func TestPlaceOrderSignsExactlyTransmittedBodyAndPreservesLinkID(t *testing.T) {
	fixed := time.UnixMilli(1700000000123)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		wantBody := `{"category":"linear","symbol":"BTCUSDT","side":"Buy","orderType":"Limit","qty":"0.001","price":"10000","timeInForce":"GTC","orderLinkId":"client-1"}`
		if got := string(body); got != wantBody {
			t.Errorf("body = %q, want %q", got, wantBody)
		}
		wantSign := Sign(fixed.UnixMilli(), "test-api-key", 5000, string(body), "test-secret")
		assertHeader(t, r, "X-BAPI-SIGN", wantSign)
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"orderId":"exchange-1","orderLinkId":"client-1"}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key", "test-secret", WithClock(func() time.Time { return fixed }))
	ack, _, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Category: "linear", Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit",
		Qty: "0.001", Price: "10000", TimeInForce: "GTC", OrderLinkID: "client-1",
	})
	if err != nil {
		t.Fatalf("PlaceOrder() error: %v", err)
	}
	if ack.OrderID != "exchange-1" || ack.OrderLinkID != "client-1" {
		t.Fatalf("unexpected ACK: %+v", ack)
	}
}

func TestAPIErrorAndRateLimitEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Bapi-Limit", "10")
		w.Header().Set("X-Bapi-Limit-Status", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"retCode":10006,"retMsg":"Too many visits","result":{}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "secret")
	_, _, err := client.Orders(context.Background(), "linear", "BTCUSDT", "", "", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 10006 || apiErr.HTTPStatus != http.StatusTooManyRequests || apiErr.RateLimit.Remaining != 0 {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestMalformedJSONIsExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{not-json`)
	}))
	defer server.Close()
	client := NewClient(server.URL, "", "")
	_, _, err := client.ServerTime(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode Bybit response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlaceOrderTransportFailureIsNotRetried(t *testing.T) {
	transport := &countingTransport{err: errors.New("fixture timeout")}
	client := NewClient("https://fixture.invalid", "key", "secret", WithHTTPClient(&http.Client{Transport: transport}))
	_, _, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{Category: "linear", Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "0.001", Price: "100", OrderLinkID: "client-uncertain"})
	var uncertain *UncertainSubmissionError
	if !errors.As(err, &uncertain) || uncertain.OrderLinkID != "client-uncertain" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if transport.calls != 1 {
		t.Fatalf("transport called %d times, want exactly once", transport.calls)
	}
}

func TestPlaceOrderRejectsInvalidInputBeforeTransport(t *testing.T) {
	transport := &countingTransport{err: errors.New("must not be called")}
	client := NewClient("https://fixture.invalid", "key", "secret", WithHTTPClient(&http.Client{Transport: transport}))
	tests := []PlaceOrderRequest{{Category: "linear", Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "0", Price: "100", OrderLinkID: "id"}, {Category: "linear", Symbol: "BTCUSDT", Side: "bad", OrderType: "Limit", Qty: "1", Price: "100", OrderLinkID: "id"}, {Category: "linear", Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "1", OrderLinkID: "id"}, {Category: "linear", Symbol: "BTCUSDT", Side: "Buy", OrderType: "Market", Qty: "1", OrderLinkID: strings.Repeat("x", 37)}}
	for _, request := range tests {
		if _, _, err := client.PlaceOrder(context.Background(), request); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	if transport.calls != 0 {
		t.Fatalf("invalid requests reached transport %d times", transport.calls)
	}
}

func TestClientStatsCountSuccessAndFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok/v5/market/time" {
			_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"timeSecond":"1700000000"}}`)
			return
		}
		_, _ = io.WriteString(w, `not-json`)
	}))
	defer server.Close()
	success := NewClient(server.URL+"/ok", "", "")
	_, _, _ = success.ServerTime(context.Background())
	stats := success.Stats()
	if stats.Requests != 1 || stats.Errors != 0 || stats.TotalLatency <= 0 {
		t.Fatalf("success stats=%+v", stats)
	}
	failure := NewClient(server.URL, "", "")
	_, _, _ = failure.ServerTime(context.Background())
	stats = failure.Stats()
	if stats.Requests != 1 || stats.Errors != 1 || stats.TotalLatency <= 0 {
		t.Fatalf("failure stats=%+v", stats)
	}
}

func TestGenerateOrderLinkIDUniqueAndWithinLimit(t *testing.T) {
	const count = 1000
	seen := make(map[string]struct{}, count)
	for i := 0; i < count; i++ {
		id, err := GenerateOrderLinkID(time.Unix(1700000000, 0))
		if err != nil {
			t.Fatal(err)
		}
		if len(id) > 36 {
			t.Fatalf("ID length = %d: %q", len(id), id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ID: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func assertHeader(t *testing.T, request *http.Request, name, want string) {
	t.Helper()
	if got := request.Header.Get(name); got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

type countingTransport struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	return nil, t.err
}
