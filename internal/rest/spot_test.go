package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpotReadEndpointsPreserveExactDecimalsAndUseSpotScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("category"); got != "spot" {
			t.Errorf("category = %q, want spot", got)
		}
		if got := r.URL.Query().Get("symbol"); got != "BTCUSDT" {
			t.Errorf("symbol = %q, want BTCUSDT", got)
		}
		switch r.URL.Path {
		case "/v5/market/orderbook":
			if got := r.URL.Query().Get("limit"); got != "50" {
				t.Errorf("limit = %q, want 50", got)
			}
			_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"s":"BTCUSDT","b":[["100000.01","0.12345678"]],"a":[["100000.02","0.87654321"]],"u":42,"seq":43,"ts":1789000000123},"time":1789000000124}`)
		case "/v5/account/fee-rate":
			assertAuthenticatedSpotRequest(t, r)
			_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"list":[{"symbol":"BTCUSDT","takerFeeRate":"0.00100001","makerFeeRate":"0.0009"}]},"time":1789000000124}`)
		case "/v5/order/spot-borrow-check":
			assertAuthenticatedSpotRequest(t, r)
			if got := r.URL.Query().Get("side"); got != "Buy" {
				t.Errorf("side = %q, want Buy", got)
			}
			_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"symbol":"BTCUSDT","side":"Buy","maxTradeQty":"9","maxTradeAmount":"900000","spotMaxTradeQty":"0.1","spotMaxTradeAmount":"10000.00000001"},"time":1789000000124}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, "fixture-key", "fixture-secret", WithHTTPClient(server.Client()))

	book, _, err := client.SpotOrderBook(context.Background(), "BTCUSDT", 50)
	if err != nil || book.Symbol != "BTCUSDT" || book.Bids[0][0] != "100000.01" || book.Asks[0][1] != "0.87654321" || book.TimestampMS != 1789000000123 {
		t.Fatalf("SpotOrderBook() = %+v, %v", book, err)
	}
	fees, _, err := client.SpotFeeRates(context.Background(), "BTCUSDT")
	if err != nil || len(fees) != 1 || fees[0].TakerFeeRate != "0.00100001" {
		t.Fatalf("SpotFeeRates() = %+v, %v", fees, err)
	}
	capacity, _, err := client.SpotBorrowCapacity(context.Background(), "BTCUSDT", "Buy")
	if err != nil || capacity.SpotMaxTradeAmount != "10000.00000001" || capacity.MaxTradeAmount != "900000" {
		t.Fatalf("SpotBorrowCapacity() = %+v, %v", capacity, err)
	}
}

func TestSpotOrderSubmissionIncludesExplicitNoLeverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["category"] != "spot" || body["timeInForce"] != "IOC" || body["isLeverage"] != float64(0) {
			t.Errorf("order body = %+v", body)
		}
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"orderId":"venue-1","orderLinkId":"vw-1"},"time":1789000000124}`)
	}))
	defer server.Close()
	client := NewClient(server.URL, "fixture-key", "fixture-secret", WithHTTPClient(server.Client()))
	zero := 0
	ack, _, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Category: "spot", Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "0.001", Price: "100000", TimeInForce: "IOC", OrderLinkID: "vw-1", IsLeverage: &zero,
	})
	if err != nil || ack.OrderID != "venue-1" {
		t.Fatalf("PlaceOrder() = %+v, %v", ack, err)
	}
}

func TestSpotReadEndpointsRejectInvalidInputsLocally(t *testing.T) {
	client := NewClient("https://fixture.invalid", "key", "secret")
	if _, _, err := client.SpotOrderBook(context.Background(), "", 50); err == nil {
		t.Fatal("SpotOrderBook() empty symbol error = nil")
	}
	if _, _, err := client.SpotOrderBook(context.Background(), "BTCUSDT", 0); err == nil {
		t.Fatal("SpotOrderBook() invalid limit error = nil")
	}
	if _, _, err := client.SpotFeeRates(context.Background(), ""); err == nil {
		t.Fatal("SpotFeeRates() empty symbol error = nil")
	}
	if _, _, err := client.SpotBorrowCapacity(context.Background(), "BTCUSDT", "Long"); err == nil {
		t.Fatal("SpotBorrowCapacity() invalid side error = nil")
	}
}

func assertAuthenticatedSpotRequest(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Header.Get("X-BAPI-API-KEY") != "fixture-key" || request.Header.Get("X-BAPI-SIGN") == "" {
		t.Errorf("authenticated headers are missing")
	}
}
