package rest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpotInstrumentUsesBasePrecisionAsQuantityStep(t *testing.T) {
	// Regression: ISSUE-003 — Bybit Spot stopped returning qtyStep and quotes became unavailable.
	// Found by /qa on 2026-09-10
	// Report: .gstack/qa-reports/qa-report-venue-trade-alert-net-2026-09-10.md
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/market/instruments-info" || r.URL.Query().Get("category") != "spot" {
			t.Fatalf("unexpected instrument request: %s", r.URL.String())
		}
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"list":[{"symbol":"BTCUSDT","status":"Trading","baseCoin":"BTC","quoteCoin":"USDT","priceFilter":{"tickSize":"0.1"},"lotSizeFilter":{"basePrecision":"0.000001","minOrderQty":"0.000001","maxOrderQty":"10","minOrderAmt":"5"}}]}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "fixture-key", "fixture-secret", WithHTTPClient(server.Client()))
	instruments, _, err := client.Instruments(t.Context(), "spot", "BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if len(instruments) != 1 || instruments[0].LotSizeFilter.QtyStep != "0.000001" {
		t.Fatalf("Spot quantity step was not normalized: %+v", instruments)
	}
}
