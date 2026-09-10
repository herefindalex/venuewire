package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"venuewire/internal/accountstate"
	"venuewire/internal/deribit"
	"venuewire/internal/domain"
)

func TestStreamState(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Second).UnixMilli()
	old := now.Add(-10 * time.Second).UnixMilli()
	tests := []struct {
		name       string
		connected  bool
		eventMS    int64
		staleAfter time.Duration
		want       string
	}{
		{name: "initial connection", connected: false, staleAfter: 3 * time.Second, want: "CONNECTING"},
		{name: "live", connected: true, eventMS: recent, staleAfter: 3 * time.Second, want: "LIVE"},
		{name: "stale", connected: true, eventMS: old, staleAfter: 3 * time.Second, want: "STALE"},
		{name: "reconnecting", connected: false, eventMS: recent, staleAfter: 3 * time.Second, want: "RECONNECTING"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := streamState(test.connected, test.eventMS, now, test.staleAfter); got != test.want {
				t.Fatalf("streamState() = %q, want %q", got, test.want)
			}
		})
	}
}

type streamMetricProvider struct{ venue domain.Venue }

func (p streamMetricProvider) Venue() domain.Venue { return p.venue }
func (p streamMetricProvider) Snapshot(context.Context) (accountstate.Snapshot, error) {
	return accountstate.Snapshot{}, nil
}

func TestRecordDeribitOrderMetric(t *testing.T) {
	manager, err := accountstate.NewManager([]accountstate.Provider{streamMetricProvider{venue: domain.VenueDeribit}}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ackAt := time.Date(2026, 9, 10, 18, 0, 0, 0, time.UTC)
	manager.RecordOrderSubmission(domain.VenueDeribit, "client-1", "venue-1", ackAt, time.Millisecond, true, false)
	if err := recordDeribitOrderMetric(manager, deribit.WSNotification{
		Channel: "user.orders.BTC_USDC.raw", ReceivedAt: ackAt.Add(10 * time.Millisecond),
		Data: json.RawMessage(`{"order_id":"venue-1","label":"client-1"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := recordDeribitOrderMetric(manager, deribit.WSNotification{
		Channel: "user.trades.BTC_USDC.raw", ReceivedAt: ackAt.Add(20 * time.Millisecond),
		Data: json.RawMessage(`[{"trade_id":"trade-1","order_id":"venue-1"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	health := manager.Health()[0]
	if !health.HasFirstOrderEvent || health.FirstOrderEventLatency != 10*time.Millisecond || !health.HasFirstExecutionEvent || health.FirstExecutionEventLatency != 20*time.Millisecond {
		t.Fatalf("Deribit event metrics = %+v", health)
	}
}

func TestDecodeDeribitUSDPrice(t *testing.T) {
	received := time.Date(2026, 9, 10, 12, 0, 1, 0, time.UTC)
	event := deribit.WSNotification{
		Channel:    "deribit_price_index.btc_usd",
		Data:       json.RawMessage(`{"timestamp":1789056000123,"price":112345.67,"index_name":"btc_usd"}`),
		ReceivedAt: received,
	}
	price, relevant, err := decodeDeribitUSDPrice(event)
	if err != nil {
		t.Fatal(err)
	}
	if !relevant || price.Asset != "BTC" || price.Value != "112345.67" || price.Source != "Deribit Testnet btc_usd index" || !price.ReceivedAt.Equal(received) || price.ObservedAt.UnixMilli() != 1789056000123 {
		t.Fatalf("decoded price = %+v relevant=%t", price, relevant)
	}
}

func TestDecodeDeribitUSDPriceIgnoresOtherChannelsAndRejectsInvalidPrice(t *testing.T) {
	if _, relevant, err := decodeDeribitUSDPrice(deribit.WSNotification{Channel: "book.BTC_USDC.none.50.100ms"}); err != nil || relevant {
		t.Fatalf("book event = relevant %t err %v", relevant, err)
	}
	for _, data := range []string{`{"price":0}`, `{"price":"invalid"}`, `{`} {
		_, relevant, err := decodeDeribitUSDPrice(deribit.WSNotification{
			Channel: "deribit_price_index.eth_usd", Data: json.RawMessage(data), ReceivedAt: time.Now(),
		})
		if !relevant || err == nil {
			t.Fatalf("invalid data %q = relevant %t err %v", data, relevant, err)
		}
	}
}

func TestValuationQueueKeepsLatestPricePerAsset(t *testing.T) {
	queue := newValuationQueue()
	queue.Store(accountstate.USDPrice{Asset: "BTC", Value: "100"})
	queue.Store(accountstate.USDPrice{Asset: "ETH", Value: "10"})
	queue.Store(accountstate.USDPrice{Asset: "BTC", Value: "101"})
	batch := queue.Drain()
	if len(batch) != 2 || batch[0].Asset != "BTC" || batch[0].Value != "101" || batch[1].Asset != "ETH" {
		t.Fatalf("valuation batch = %+v", batch)
	}
	if remaining := queue.Drain(); len(remaining) != 0 {
		t.Fatalf("queue was not drained: %+v", remaining)
	}
}

func TestUnixTimeZeroAndUTC(t *testing.T) {
	if got := unixTime(0); !got.IsZero() {
		t.Fatalf("unixTime(0) = %v", got)
	}
	got := unixTime(1700000000000)
	if got.Location() != time.UTC || got.UnixMilli() != 1700000000000 {
		t.Fatalf("unixTime() = %v", got)
	}
}

func TestUnixMillisecondsZeroAndTimestamp(t *testing.T) {
	if got := unixMilliseconds(time.Time{}); got != 0 {
		t.Fatalf("unixMilliseconds(zero) = %d", got)
	}
	want := time.Date(2026, 9, 10, 12, 0, 1, 123000000, time.FixedZone("fixture", -4*60*60))
	if got := unixMilliseconds(want); got != want.UnixMilli() {
		t.Fatalf("unixMilliseconds(timestamp) = %d, want %d", got, want.UnixMilli())
	}
}
