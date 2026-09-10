package accountstate

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"venuewire/internal/domain"
)

type fakeProvider struct {
	venue    domain.Venue
	snapshot Snapshot
	err      error
	calls    int
}

type blockingProvider struct {
	venue   domain.Venue
	started chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (f *blockingProvider) Venue() domain.Venue { return f.venue }

func (f *blockingProvider) Snapshot(context.Context) (Snapshot, error) {
	f.calls.Add(1)
	select {
	case f.started <- struct{}{}:
	default:
	}
	<-f.release
	return Snapshot{}, nil
}

func (f *fakeProvider) Venue() domain.Venue { return f.venue }

func (f *fakeProvider) Snapshot(context.Context) (Snapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func TestManagerRefreshCachesSortedCopyAndCapacity(t *testing.T) {
	now := time.Date(2026, 9, 9, 14, 0, 0, 0, time.UTC)
	provider := &fakeProvider{
		venue: domain.VenueBybit,
		snapshot: Snapshot{Assets: []Asset{
			{Asset: "USDT", AvailableToTrade: "100", AvailableToTradeAsOf: now, AvailableStatus: "verified", Quality: "snapshot"},
			{Asset: "BTC", AvailableToTrade: "0.5", AvailableToTradeAsOf: now, AvailableStatus: "derived", Quality: "snapshot"},
		}},
	}
	manager, err := NewManager([]Provider{provider}, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	manager.now = func() time.Time { return now }

	if err := manager.Refresh(context.Background(), domain.VenueBybit); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	snapshot, ok := manager.Snapshot(domain.VenueBybit)
	if !ok {
		t.Fatal("Snapshot() ok = false")
	}
	if snapshot.Revision != 1 || snapshot.Environment != "testnet" || !snapshot.SnapshotAsOf.Equal(now) {
		t.Fatalf("snapshot metadata = %+v", snapshot)
	}
	if len(snapshot.Assets) != 2 || snapshot.Assets[0].Asset != "BTC" || snapshot.Assets[1].Asset != "USDT" {
		t.Fatalf("sorted assets = %+v", snapshot.Assets)
	}
	amount, revision, asOf, ok := manager.Capacity(domain.VenueBybit, "BTC")
	if !ok || amount != "0.5" || revision != 1 || !asOf.Equal(now) {
		t.Fatalf("Capacity() = (%q, %d, %s, %t)", amount, revision, asOf, ok)
	}

	// Returned snapshots must not mutate the manager's cached state.
	snapshot.Assets[0].AvailableToTrade = "999"
	snapshot.UnpricedAssets = append(snapshot.UnpricedAssets, "MUTATED")
	again, _ := manager.Snapshot(domain.VenueBybit)
	if again.Assets[0].AvailableToTrade != "0.5" || len(again.UnpricedAssets) != 0 {
		t.Fatalf("cached snapshot was mutated through returned copy: %+v", again)
	}

	health := manager.Health()
	if len(health) != 1 || health[0].REST != "LIVE" || health[0].AccountSync != "SYNCED" || health[0].RequestErrors != 0 {
		t.Fatalf("Health() = %+v", health)
	}
}

func TestManagerMarksOldSnapshotStaleAndBlocksCapacity(t *testing.T) {
	now := time.Date(2026, 9, 9, 15, 0, 0, 0, time.UTC)
	provider := &fakeProvider{venue: domain.VenueBybit, snapshot: Snapshot{Assets: []Asset{{
		Asset: "USDT", AvailableToTrade: "10", AvailableStatus: "verified", Quality: "snapshot",
	}}}}
	manager, err := NewManager([]Provider{provider}, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	manager.now = func() time.Time { return now }
	if err := manager.Refresh(context.Background(), domain.VenueBybit); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	now = now.Add(6 * time.Second)
	snapshot, ok := manager.Snapshot(domain.VenueBybit)
	if !ok || snapshot.Assets[0].Quality != "stale" {
		t.Fatalf("stale Snapshot() = (%+v, %t)", snapshot, ok)
	}
	if amount, _, _, ok := manager.Capacity(domain.VenueBybit, "USDT"); ok || amount != "" {
		t.Fatalf("stale Capacity() = (%q, %t), want unavailable", amount, ok)
	}
	health := manager.Health()
	if health[0].AccountSync != "STALE" {
		t.Fatalf("AccountSync = %q, want STALE", health[0].AccountSync)
	}
}

func TestManagerRefreshFailureRetainsSnapshotAndReportsSafeHealth(t *testing.T) {
	now := time.Date(2026, 9, 9, 16, 0, 0, 0, time.UTC)
	provider := &fakeProvider{venue: domain.VenueDeribit, snapshot: Snapshot{Assets: []Asset{{Asset: "ETH", Quality: "unpriced"}}}}
	manager, err := NewManager([]Provider{provider}, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	manager.now = func() time.Time { return now }
	if err := manager.Refresh(context.Background(), domain.VenueDeribit); err != nil {
		t.Fatalf("initial Refresh() error = %v", err)
	}

	want := errors.New("private account request failed")
	provider.err = want
	now = now.Add(6 * time.Second)
	if err := manager.Refresh(context.Background(), domain.VenueDeribit); !errors.Is(err, want) {
		t.Fatalf("failed Refresh() error = %v, want %v", err, want)
	}
	snapshot, ok := manager.Snapshot(domain.VenueDeribit)
	if !ok || snapshot.Revision != 1 || snapshot.Assets[0].Quality != "stale" {
		t.Fatalf("retained Snapshot() = (%+v, %t)", snapshot, ok)
	}
	health := manager.Health()
	if health[0].REST != "ERROR" || health[0].AccountSync != "STALE" || health[0].RequestErrors != 1 || health[0].LastPublicError == "" {
		t.Fatalf("Health() after refresh failure = %+v", health[0])
	}
}

func TestManagerInitialFailureAndUnknownVenue(t *testing.T) {
	want := errors.New("unavailable")
	provider := &fakeProvider{venue: domain.VenueBybit, err: want}
	manager, err := NewManager([]Provider{provider}, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := manager.Refresh(context.Background(), domain.VenueBybit); !errors.Is(err, want) {
		t.Fatalf("Refresh() error = %v, want %v", err, want)
	}
	if _, ok := manager.Snapshot(domain.VenueBybit); ok {
		t.Fatal("Snapshot() after initial failure ok = true")
	}
	if got := manager.Health()[0].AccountSync; got != "ERROR" {
		t.Fatalf("AccountSync = %q, want ERROR", got)
	}
	if err := manager.Refresh(context.Background(), domain.VenueDeribit); err == nil {
		t.Fatal("Refresh() unknown venue error = nil")
	}
}

func TestManagerCoalescesConcurrentVenueRefresh(t *testing.T) {
	provider := &blockingProvider{
		venue: domain.VenueBybit, started: make(chan struct{}, 1), release: make(chan struct{}),
	}
	manager, err := NewManager([]Provider{provider}, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- manager.Refresh(context.Background(), domain.VenueBybit) }()
	<-provider.started

	waiterContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Refresh(waiterContext, domain.VenueBybit); !errors.Is(err, context.Canceled) {
		t.Fatalf("coalesced waiter error = %v, want context canceled", err)
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls while refresh is active = %d, want 1", calls)
	}
	close(provider.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
}

func TestNewManagerRejectsInvalidConfiguration(t *testing.T) {
	provider := &fakeProvider{venue: domain.VenueBybit}
	tests := []struct {
		name      string
		providers []Provider
		interval  time.Duration
		staleAge  time.Duration
	}{
		{name: "zero interval", interval: 0, staleAge: time.Second},
		{name: "zero stale age", interval: time.Second, staleAge: 0},
		{name: "unsupported venue", providers: []Provider{&fakeProvider{venue: "other"}}, interval: time.Second, staleAge: time.Second},
		{name: "duplicate venue", providers: []Provider{provider, provider}, interval: time.Second, staleAge: time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewManager(test.providers, test.interval, test.staleAge); err == nil {
				t.Fatal("NewManager() error = nil")
			}
		})
	}
}

func TestManagerStreamHealthPublishesOnlyChanges(t *testing.T) {
	provider := &fakeProvider{venue: domain.VenueBybit}
	manager, err := NewManager([]Provider{provider}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var events atomic.Int64
	manager.OnEvent = func(eventType string, venue domain.Venue, _ time.Time) {
		if eventType != "venue.health.updated" || venue != domain.VenueBybit {
			t.Fatalf("event = (%q, %q)", eventType, venue)
		}
		events.Add(1)
	}
	eventAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	manager.UpdatePublicWS(domain.VenueBybit, "LIVE", eventAt, 1)
	manager.UpdatePublicWS(domain.VenueBybit, "LIVE", eventAt, 1)
	manager.UpdatePublicWS(domain.VenueBybit, "LIVE", eventAt.Add(time.Second), 1)
	manager.UpdatePrivateWS(domain.VenueBybit, "LIVE", eventAt, 2)
	manager.UpdatePrivateWS(domain.VenueBybit, "LIVE", eventAt, 2)
	manager.UpdatePrivateWS(domain.VenueBybit, "LIVE", eventAt.Add(time.Second), 2)
	manager.UpdatePublicWS(domain.VenueBybit, "LIVE", eventAt.Add(-time.Second), 1)

	if got := events.Load(); got != 2 {
		t.Fatalf("published health events = %d, want 2", got)
	}
	health := manager.Health()[0]
	if health.PublicWS != "LIVE" || health.PrivateWS != "LIVE" || health.Reconnects != 3 {
		t.Fatalf("health = %+v", health)
	}
	if !health.MarketEventAt.Equal(eventAt.Add(time.Second)) || !health.LastEventAt.Equal(eventAt.Add(time.Second)) {
		t.Fatalf("health event timestamps were not updated: %+v", health)
	}
}

func TestManagerRefreshCallbackCanReadManager(t *testing.T) {
	provider := &fakeProvider{venue: domain.VenueBybit, snapshot: Snapshot{SnapshotAsOf: time.Now().UTC()}}
	manager, err := NewManager([]Provider{provider}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	manager.OnEvent = func(string, domain.Venue, time.Time) { _ = manager.Health() }
	done := make(chan error, 1)
	go func() { done <- manager.Refresh(context.Background(), domain.VenueBybit) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Refresh deadlocked while publishing an event")
	}
}
