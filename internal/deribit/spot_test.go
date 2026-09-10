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
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"instrument_name":"ETH_BTC","kind":"spot","base_currency":"ETH","counter_currency":"BTC","quote_currency":"BTC","tick_size":0.00000001,"amount_step":0.0001,"min_trade_amount":0.001,"maker_commission":0,"taker_commission":0.001,"is_active":true}}`)
		case "/api/v2/public/get_order_book":
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"instrument_name":"ETH_BTC","timestamp":1789000000123,"change_id":42,"bids":[[0.05000001,1.23456789]],"asks":[[0.05000002,2.34567891]]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/api/v2", "", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	instrument, err := client.Instrument(context.Background(), "ETH_BTC")
	if err != nil || instrument.Kind != "spot" || instrument.AmountStep.String() != "0.0001" || instrument.TakerCommission.String() != "0.001" {
		t.Fatalf("Instrument() = %+v, %v", instrument, err)
	}
	book, err := client.SpotOrderBook(context.Background(), "ETH_BTC", 20)
	if err != nil || book.InstrumentName != "ETH_BTC" || book.Bids[0][0].String() != "0.05000001" || book.Asks[0][1].String() != "2.34567891" || book.TimestampMS != 1789000000123 {
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
