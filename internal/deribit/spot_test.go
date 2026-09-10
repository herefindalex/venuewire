package deribit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpotInstrumentAndOrderBookPreserveExactMetadata(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/public/get_instrument":
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"instrument_name":"BTC_USDC","kind":"spot","base_currency":"BTC","counter_currency":"USDC","quote_currency":"USDC","tick_size":1.0,"amount_step":0.0001,"min_trade_amount":0.0001,"maker_commission":0,"taker_commission":0.001,"is_active":true}}`)
		case "/api/v2/public/get_order_book":
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"instrument_name":"BTC_USDC","timestamp":1789000000123,"change_id":42,"bids":[[78091.0,0.2345],[78090.0,1.0]],"asks":[[78141.0,0.3456],[78142.0,1.0]]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/api/v2", "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	instrument, err := client.Instrument(context.Background(), "BTC_USDC")
	if err != nil || instrument.Kind != "spot" || instrument.AmountStep.String() != "0.0001" || instrument.TakerCommission.String() != "0.001" {
		t.Fatalf("Instrument() = %+v, %v", instrument, err)
	}
	book, err := client.SpotOrderBook(context.Background(), "BTC_USDC", 20)
	if err != nil || book.InstrumentName != "BTC_USDC" || book.Bids[0][0].String() != "78091.0" || book.Asks[0][1].String() != "0.3456" || book.TimestampMS != 1789000000123 {
		t.Fatalf("SpotOrderBook() = %+v, %v", book, err)
	}
}

func TestDeribitSpotOrderBookRejectsInvalidInput(t *testing.T) {
	client, err := NewClient("https://fixture.invalid/api/v2", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SpotOrderBook(context.Background(), "", 20); err == nil {
		t.Fatal("SpotOrderBook() empty instrument error = nil")
	}
	if _, err := client.SpotOrderBook(context.Background(), "ETH_BTC", 0); err == nil {
		t.Fatal("SpotOrderBook() invalid depth error = nil")
	}
}
