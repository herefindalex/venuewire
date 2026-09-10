package accountstate

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"venuewire/internal/domain"
)

type Asset struct {
	Asset                    string    `json:"asset"`
	Balance                  string    `json:"balance"`
	Equity                   string    `json:"equity,omitempty"`
	Locked                   string    `json:"locked,omitempty"`
	Liability                string    `json:"liability,omitempty"`
	AvailableToTrade         string    `json:"availableToTrade,omitempty"`
	AvailableToTradeAsOf     time.Time `json:"availableToTradeAsOf,omitempty"`
	AvailableStatus          string    `json:"availableStatus"`
	ValuationQuantity        string    `json:"valuationQuantity,omitempty"`
	QuantityBasis            string    `json:"quantityBasis"`
	ExchangeReportedUSDValue string    `json:"exchangeReportedUsdValue,omitempty"`
	USDValue                 string    `json:"usdValue,omitempty"`
	PriceSource              string    `json:"priceSource,omitempty"`
	PriceAsOf                time.Time `json:"priceAsOf,omitempty"`
	Quality                  string    `json:"quality"`
}

type Snapshot struct {
	Venue                    domain.Venue `json:"venue"`
	Environment              string       `json:"environment"`
	AccountAlias             string       `json:"accountAlias"`
	Revision                 uint64       `json:"revision"`
	SnapshotAsOf             time.Time    `json:"snapshotAsOf"`
	ExchangeReportedTotalUSD string       `json:"exchangeReportedTotalUsd,omitempty"`
	LocalMarkedTotalUSD      string       `json:"totalUsd,omitempty"`
	PricedSubtotalUSD        string       `json:"pricedSubtotalUsd,omitempty"`
	ValuationBasis           string       `json:"valuationBasis"`
	Completeness             string       `json:"completeness"`
	UnpricedAssets           []string     `json:"unpricedAssets,omitempty"`
	Assets                   []Asset      `json:"assets"`
}

type Provider interface {
	Venue() domain.Venue
	Snapshot(context.Context) (Snapshot, error)
}

type VenueHealth struct {
	OrderRequestRTT            time.Duration `json:"-"`
	FirstOrderEventLatency     time.Duration `json:"-"`
	FirstExecutionEventLatency time.Duration `json:"-"`
	HasFirstOrderEvent         bool          `json:"-"`
	HasFirstExecutionEvent     bool          `json:"-"`
	OrderRequestErrors         uint64        `json:"orderRequestErrors"`
	RateLimitState             string        `json:"rateLimitState"`
	ReconciliationStatus       string        `json:"reconciliationStatus"`
	LastPublicReceiveAt        time.Time     `json:"lastPublicReceiveAt,omitempty"`
	LastPrivateReceiveAt       time.Time     `json:"lastPrivateReceiveAt,omitempty"`
	LastPublicEventAt          time.Time     `json:"lastPublicEventAt,omitempty"`
	LastPrivateEventAt         time.Time     `json:"lastPrivateEventAt,omitempty"`
	Venue                      domain.Venue  `json:"venue"`
	REST                       string        `json:"rest"`
	PublicWS                   string        `json:"publicWs"`
	PrivateWS                  string        `json:"privateWs"`
	AccountSync                string        `json:"accountSync"`
	LastRESTAt                 time.Time     `json:"lastRestAt,omitempty"`
	LastEventAt                time.Time     `json:"lastEventAt,omitempty"`
	MarketEventAt              time.Time     `json:"marketEventAt,omitempty"`
	Reconnects                 uint64        `json:"reconnects"`
	PublicReconnects           uint64        `json:"-"`
	PrivateReconnects          uint64        `json:"-"`
	LastReconcileAt            time.Time     `json:"lastReconcileAt,omitempty"`
	RequestErrors              uint64        `json:"requestErrors"`
	Discrepancies              uint64        `json:"reconciliationDiscrepancies"`
	LastRequestRTT             time.Duration `json:"-"`
	LastPublicError            string        `json:"-"`
}

type Manager struct {
	pendingOrderEvents map[domain.Venue]map[string]*pendingOrderTiming
	orderTimings       map[domain.Venue]map[string]*orderTiming
	prices             map[string]USDPrice
	valuationMaxAge    time.Duration
	mu                 sync.RWMutex
	providers          map[domain.Venue]Provider
	snapshots          map[domain.Venue]Snapshot
	health             map[domain.Venue]VenueHealth
	revision           uint64
	refreshMu          sync.Mutex
	refreshes          map[domain.Venue]*refreshCall
	interval           time.Duration
	staleAge           time.Duration
	now                func() time.Time
	OnEvent            func(string, domain.Venue, time.Time)
}

type refreshCall struct {
	done chan struct{}
	err  error
}

func NewManager(providers []Provider, interval, staleAge time.Duration) (*Manager, error) {
	if interval <= 0 || staleAge <= 0 {
		return nil, errors.New("account refresh and stale durations must be positive")
	}
	manager := &Manager{
		pendingOrderEvents: make(map[domain.Venue]map[string]*pendingOrderTiming),
		orderTimings:       make(map[domain.Venue]map[string]*orderTiming),
		prices:             make(map[string]USDPrice),
		valuationMaxAge:    15 * time.Second,
		providers:          make(map[domain.Venue]Provider),
		snapshots:          make(map[domain.Venue]Snapshot),
		health:             make(map[domain.Venue]VenueHealth),
		refreshes:          make(map[domain.Venue]*refreshCall),
		interval:           interval,
		staleAge:           staleAge,
		now:                time.Now,
	}
	for _, provider := range providers {
		if provider == nil || (provider.Venue() != domain.VenueBybit && provider.Venue() != domain.VenueDeribit) {
			return nil, errors.New("account provider has an invalid venue")
		}
		if _, exists := manager.providers[provider.Venue()]; exists {
			return nil, errors.New("duplicate account provider")
		}
		manager.providers[provider.Venue()] = provider
		manager.health[provider.Venue()] = VenueHealth{Venue: provider.Venue(), REST: "UNAVAILABLE", PublicWS: "UNAVAILABLE", PrivateWS: "UNAVAILABLE", AccountSync: "SYNCING", RateLimitState: "UNKNOWN", ReconciliationStatus: "IDLE"}
		manager.orderTimings[provider.Venue()] = make(map[string]*orderTiming)
		manager.pendingOrderEvents[provider.Venue()] = make(map[string]*pendingOrderTiming)
	}
	return manager, nil
}

func (m *Manager) Run(ctx context.Context) {
	m.RefreshAll(ctx)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.RefreshAll(ctx)
		}
	}
}

func (m *Manager) RefreshAll(ctx context.Context) {
	for venue := range m.providers {
		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = m.Refresh(refreshCtx, venue)
		cancel()
	}
}

func (m *Manager) Refresh(ctx context.Context, venue domain.Venue) error {
	m.refreshMu.Lock()
	if existing := m.refreshes[venue]; existing != nil {
		m.refreshMu.Unlock()
		select {
		case <-existing.done:
			return existing.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	m.refreshes[venue] = call
	m.refreshMu.Unlock()

	call.err = m.refresh(ctx, venue)
	m.refreshMu.Lock()
	delete(m.refreshes, venue)
	close(call.done)
	m.refreshMu.Unlock()
	return call.err
}

func (m *Manager) refresh(ctx context.Context, venue domain.Venue) error {
	m.mu.RLock()
	provider := m.providers[venue]
	m.mu.RUnlock()
	if provider == nil {
		return errors.New("account venue is unavailable")
	}
	started := time.Now()
	snapshot, err := provider.Snapshot(ctx)
	finished := m.now()
	m.mu.Lock()
	health := m.health[venue]
	health.LastRequestRTT = time.Since(started)
	if err != nil {
		health.REST = "ERROR"
		health.RequestErrors++
		health.LastPublicError = "Account refresh failed."
		if current, exists := m.snapshots[venue]; exists && finished.Sub(current.SnapshotAsOf) > m.staleAge {
			health.AccountSync = "STALE"
		} else if !exists {
			health.AccountSync = "ERROR"
		}
		m.health[venue] = health
		m.mu.Unlock()
		if m.OnEvent != nil {
			m.OnEvent("venue.health.updated", venue, finished)
		}
		return err
	}
	m.revision++
	snapshot.Revision = m.revision
	snapshot.Venue = venue
	snapshot.Environment = "testnet"
	if snapshot.SnapshotAsOf.IsZero() {
		snapshot.SnapshotAsOf = finished
	}
	sort.Slice(snapshot.Assets, func(i, j int) bool { return snapshot.Assets[i].Asset < snapshot.Assets[j].Asset })
	m.applyValuationLocked(&snapshot, finished)
	m.snapshots[venue] = cloneSnapshot(snapshot)
	health.REST = "LIVE"
	health.AccountSync = "SYNCED"
	health.LastRESTAt = finished
	health.LastReconcileAt = finished
	health.LastPublicError = ""
	m.health[venue] = health
	m.mu.Unlock()
	if m.OnEvent != nil {
		m.OnEvent("account.updated", venue, finished)
		m.OnEvent("venue.health.updated", venue, finished)
	}
	return nil
}

func (m *Manager) Snapshot(venue domain.Venue) (Snapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot, ok := m.snapshots[venue]
	if !ok {
		return Snapshot{}, false
	}
	result := cloneSnapshot(snapshot)
	if m.now().Sub(result.SnapshotAsOf) > m.staleAge {
		for index := range result.Assets {
			result.Assets[index].Quality = "stale"
		}
	}
	return result, true
}

func (m *Manager) Capacity(venue domain.Venue, asset string) (string, uint64, time.Time, bool) {
	snapshot, ok := m.Snapshot(venue)
	if !ok || m.now().Sub(snapshot.SnapshotAsOf) > m.staleAge {
		return "", 0, time.Time{}, false
	}
	for _, item := range snapshot.Assets {
		if item.Asset == asset && (item.AvailableStatus == "verified" || item.AvailableStatus == "derived") && item.AvailableToTrade != "" {
			return item.AvailableToTrade, snapshot.Revision, item.AvailableToTradeAsOf, true
		}
	}
	return "", snapshot.Revision, snapshot.SnapshotAsOf, false
}

func (m *Manager) Health() []VenueHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.now()
	result := make([]VenueHealth, 0, len(m.health))
	for venue, value := range m.health {
		if snapshot, ok := m.snapshots[venue]; ok && now.Sub(snapshot.SnapshotAsOf) > m.staleAge {
			value.AccountSync = "STALE"
		}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Venue < result[j].Venue })
	return result
}

func (m *Manager) UpdatePublicWS(venue domain.Venue, state string, eventAt time.Time, reconnects uint64) {
	m.mu.Lock()
	health, exists := m.health[venue]
	changed := false
	if exists {
		changed = health.PublicWS != state || health.PublicReconnects != reconnects
		health.PublicWS = state
		health.PublicReconnects = reconnects
		health.Reconnects = health.PublicReconnects + health.PrivateReconnects
		if !eventAt.IsZero() {
			if health.LastPublicEventAt.IsZero() || eventAt.After(health.LastPublicEventAt) {
				health.LastPublicEventAt = eventAt
			}
			if health.LastEventAt.IsZero() || eventAt.After(health.LastEventAt) {
				health.LastEventAt = eventAt
			}
			if health.MarketEventAt.IsZero() || eventAt.After(health.MarketEventAt) {
				health.MarketEventAt = eventAt
			}
		}
		m.health[venue] = health
	}
	m.mu.Unlock()
	if exists && changed && m.OnEvent != nil {
		m.OnEvent("venue.health.updated", venue, m.now())
	}
}

func (m *Manager) UpdatePrivateWS(venue domain.Venue, state string, eventAt time.Time, reconnects uint64) {
	m.mu.Lock()
	health, exists := m.health[venue]
	changed := false
	if exists {
		changed = health.PrivateWS != state || health.PrivateReconnects != reconnects
		health.PrivateWS = state
		health.PrivateReconnects = reconnects
		health.Reconnects = health.PublicReconnects + health.PrivateReconnects
		if !eventAt.IsZero() && (health.LastEventAt.IsZero() || eventAt.After(health.LastEventAt)) {
			health.LastEventAt = eventAt
		}
		if !eventAt.IsZero() && (health.LastPrivateEventAt.IsZero() || eventAt.After(health.LastPrivateEventAt)) {
			health.LastPrivateEventAt = eventAt
		}
		m.health[venue] = health
	}
	m.mu.Unlock()
	if exists && changed && m.OnEvent != nil {
		m.OnEvent("venue.health.updated", venue, m.now())
	}
}

func (m *Manager) UpdateWSReceiveTimes(venue domain.Venue, publicAt, privateAt time.Time) {
	m.mu.Lock()
	health, exists := m.health[venue]
	if exists {
		if !publicAt.IsZero() && (health.LastPublicReceiveAt.IsZero() || publicAt.After(health.LastPublicReceiveAt)) {
			health.LastPublicReceiveAt = publicAt
		}
		if !privateAt.IsZero() && (health.LastPrivateReceiveAt.IsZero() || privateAt.After(health.LastPrivateReceiveAt)) {
			health.LastPrivateReceiveAt = privateAt
		}
		m.health[venue] = health
	}
	m.mu.Unlock()
}

func cloneSnapshot(source Snapshot) Snapshot {
	copy := source
	copy.Assets = append([]Asset(nil), source.Assets...)
	copy.UnpricedAssets = append([]string(nil), source.UnpricedAssets...)
	return copy
}
