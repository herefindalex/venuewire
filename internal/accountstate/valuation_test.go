package accountstate

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/domain"
)

func TestLiveValuationChangesMarksWithoutChangingQuantities(t *testing.T) {
	now := time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)
	provider := &fakeProvider{venue: domain.VenueBybit, snapshot: Snapshot{
		ExchangeReportedTotalUSD: "99999",
		Assets: []Asset{
			{Asset: "BTC", Balance: "0.5", ValuationQuantity: "0.5", QuantityBasis: "wallet balance", ExchangeReportedUSDValue: "49000", USDValue: "49000"},
			{Asset: "ETH", Balance: "2", ValuationQuantity: "2", QuantityBasis: "wallet balance"},
			{Asset: "USDT", Balance: "100", ValuationQuantity: "100", QuantityBasis: "wallet balance"},
			{Asset: "ZERO", Balance: "0", ValuationQuantity: "0", QuantityBasis: "wallet balance"},
		},
	}}
	manager, err := NewManager([]Provider{provider}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now }
	if err := manager.SetValuationMaxAge(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := manager.Refresh(context.Background(), domain.VenueBybit); err != nil {
		t.Fatal(err)
	}

	var valuationEvents int
	manager.OnEvent = func(eventType string, venue domain.Venue, _ time.Time) {
		if eventType == "valuation.updated" && venue == domain.VenueBybit {
			valuationEvents++
		}
	}
	if err := manager.UpdateUSDPrices([]USDPrice{
		{Asset: "BTC", Value: "100000", Source: "Deribit btc_usd index", ObservedAt: now.Add(-time.Millisecond), ReceivedAt: now},
		{Asset: "ETH", Value: "3000", Source: "Deribit eth_usd index", ObservedAt: now.Add(-2 * time.Millisecond), ReceivedAt: now},
	}, now); err != nil {
		t.Fatal(err)
	}

	snapshot, _ := manager.Snapshot(domain.VenueBybit)
	assets := assetsByName(snapshot.Assets)
	if assets["BTC"].Balance != "0.5" || assets["BTC"].ValuationQuantity != "0.5" || assets["BTC"].USDValue != "50000" {
		t.Fatalf("BTC after valuation = %+v", assets["BTC"])
	}
	if assets["BTC"].ExchangeReportedUSDValue != "49000" || snapshot.ExchangeReportedTotalUSD != "99999" {
		t.Fatalf("exchange-reported values changed: asset=%+v snapshot=%+v", assets["BTC"], snapshot)
	}
	if assets["ETH"].USDValue != "6000" || assets["ZERO"].USDValue != "0" {
		t.Fatalf("marked assets = ETH %+v ZERO %+v", assets["ETH"], assets["ZERO"])
	}
	if snapshot.LocalMarkedTotalUSD != "" || snapshot.PricedSubtotalUSD != "56000" || snapshot.Completeness != "partial" || !slices.Equal(snapshot.UnpricedAssets, []string{"USDT"}) {
		t.Fatalf("partial valuation = %+v", snapshot)
	}

	if err := manager.UpdateUSDPrices([]USDPrice{{Asset: "USDT", Value: "0.999", Source: "fixture USDT/USD", ObservedAt: now, ReceivedAt: now}}, now); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = manager.Snapshot(domain.VenueBybit)
	if snapshot.LocalMarkedTotalUSD != "56099.9" || snapshot.PricedSubtotalUSD != "56099.9" || snapshot.Completeness != "complete" || len(snapshot.UnpricedAssets) != 0 {
		t.Fatalf("complete valuation = %+v", snapshot)
	}
	if valuationEvents != 2 {
		t.Fatalf("valuation events = %d, want 2", valuationEvents)
	}

	manager.Revalue(now.Add(6 * time.Second))
	stale, _ := manager.Snapshot(domain.VenueBybit)
	if stale.LocalMarkedTotalUSD != "" || stale.PricedSubtotalUSD != "0" || stale.Completeness != "partial" || !slices.Equal(stale.UnpricedAssets, []string{"BTC", "ETH", "USDT"}) {
		t.Fatalf("stale valuation = %+v", stale)
	}
	manager.Revalue(now.Add(7 * time.Second))
	if valuationEvents != 3 {
		t.Fatalf("stale transition events = %d, want 3", valuationEvents)
	}
}

func TestValuationRejectsInvalidBatchAndIgnoresOlderPrice(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	provider := &fakeProvider{venue: domain.VenueDeribit, snapshot: Snapshot{Assets: []Asset{{Asset: "BTC", Balance: "1", ValuationQuantity: "1"}}}}
	manager, err := NewManager([]Provider{provider}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now }
	if err := manager.Refresh(context.Background(), domain.VenueDeribit); err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateUSDPrices([]USDPrice{
		{Asset: "BTC", Value: "100000", Source: "fixture", ReceivedAt: now},
		{Asset: "ETH", Value: "not-a-number", Source: "fixture", ReceivedAt: now},
	}, now); err == nil {
		t.Fatal("invalid price batch error = nil")
	}
	snapshot, _ := manager.Snapshot(domain.VenueDeribit)
	if snapshot.Assets[0].USDValue != "" {
		t.Fatalf("invalid batch partially applied: %+v", snapshot.Assets[0])
	}

	if err := manager.UpdateUSDPrices([]USDPrice{{Asset: "BTC", Value: "100000", Source: "new", ReceivedAt: now}}, now); err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateUSDPrices([]USDPrice{{Asset: "BTC", Value: "90000", Source: "old", ReceivedAt: now.Add(-time.Second)}}, now); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = manager.Snapshot(domain.VenueDeribit)
	if snapshot.Assets[0].USDValue != "100000" || snapshot.Assets[0].PriceSource != "new" {
		t.Fatalf("older price replaced current mark: %+v", snapshot.Assets[0])
	}
}

func TestSetValuationMaxAgeRejectsNonPositiveDuration(t *testing.T) {
	manager, err := NewManager([]Provider{&fakeProvider{venue: domain.VenueBybit}}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetValuationMaxAge(0); err == nil {
		t.Fatal("SetValuationMaxAge(0) error = nil")
	}
}

func assetsByName(assets []Asset) map[string]Asset {
	result := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		result[asset.Asset] = asset
	}
	return result
}
