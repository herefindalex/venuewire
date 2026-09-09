package ws

import (
	"testing"
	"time"
)

func TestDecodePublicTrades(t *testing.T) {
	received := time.UnixMilli(1700000000200).UTC()
	payload := []byte(`{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1700000000100,"data":[{"T":1700000000125,"s":"BTCUSDT","S":"Buy","v":"0.001","p":"42000.10","L":"PlusTick","i":"trade-1","BT":false},{"T":1700000000150,"s":"BTCUSDT","S":"Sell","v":"0.002","p":"42000.00","L":"MinusTick","i":"trade-2","BT":true}]}`)
	events, err := DecodePublic(payload, received)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Trade == nil || events[1].Trade == nil {
		t.Fatalf("unexpected events: %+v", events)
	}
	if events[0].Symbol != "BTCUSDT" || events[0].Trade.ID != "trade-1" || events[0].Lag != 75*time.Millisecond {
		t.Fatalf("unexpected first trade: %+v", events[0])
	}
	if !events[1].Trade.BlockTrade {
		t.Fatal("block trade flag lost")
	}
}

func TestDecodeOrderBookSnapshotAndDelta(t *testing.T) {
	received := time.UnixMilli(1700000000200).UTC()
	tests := []struct {
		name, kind, bids string
	}{
		{"snapshot", "snapshot", `[["42000.0","1.2"]]`},
		{"delta", "delta", `[["42000.0","0"]]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := []byte(`{"topic":"orderbook.50.BTCUSDT","type":"` + tt.kind + `","ts":1700000000100,"cts":1700000000120,"data":{"s":"BTCUSDT","b":` + tt.bids + `,"a":[["42000.1","0.8"]],"u":100,"seq":200}}`)
			events, err := DecodePublic(payload, received)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].OrderBook == nil {
				t.Fatalf("unexpected events: %+v", events)
			}
			book := events[0].OrderBook
			if book.UpdateType != tt.kind || book.UpdateID != 100 || book.Sequence != 200 || len(book.Bids) != 1 || book.Bids[0].Size == "" || events[0].Lag != 80*time.Millisecond {
				t.Fatalf("unexpected book: event=%+v book=%+v", events[0], book)
			}
		})
	}
}

func TestDecodeControlAndMalformedMessages(t *testing.T) {
	events, err := DecodePublic([]byte(`{"success":true,"op":"subscribe"}`), time.Now())
	if err != nil || len(events) != 0 {
		t.Fatalf("control decode: events=%v err=%v", events, err)
	}
	if _, err := DecodePublic([]byte(`{"topic":"publicTrade.BTCUSDT","data":{}}`), time.Now()); err == nil {
		t.Fatal("malformed trade payload accepted")
	}
}
