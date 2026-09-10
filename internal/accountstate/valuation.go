package accountstate

import (
	"errors"
	"math/big"
	"slices"
	"strings"
	"time"

	"venuewire/internal/domain"
)

// USDPrice is a normalized public-market price. ObservedAt is the venue event
// time; ReceivedAt is the local receive time used for freshness decisions.
type USDPrice struct {
	Asset      string
	Value      string
	Source     string
	ObservedAt time.Time
	ReceivedAt time.Time
}

func (m *Manager) SetValuationMaxAge(maxAge time.Duration) error {
	if maxAge <= 0 {
		return errors.New("valuation maximum age must be positive")
	}
	m.mu.Lock()
	m.valuationMaxAge = maxAge
	m.mu.Unlock()
	return nil
}

// UpdateUSDPrices atomically installs a batch of prices and revalues every
// cached account without changing exchange-reported balances or equity.
func (m *Manager) UpdateUSDPrices(updates []USDPrice, now time.Time) error {
	if len(updates) == 0 {
		return nil
	}
	if now.IsZero() {
		now = m.now()
	}
	normalized := make([]USDPrice, 0, len(updates))
	for _, update := range updates {
		update.Asset = strings.ToUpper(strings.TrimSpace(update.Asset))
		update.Value = strings.TrimSpace(update.Value)
		update.Source = strings.TrimSpace(update.Source)
		value, ok := rat(update.Value)
		if update.Asset == "" || update.Source == "" || !ok || value.Sign() <= 0 {
			return errors.New("USD price update is invalid")
		}
		if update.ReceivedAt.IsZero() {
			update.ReceivedAt = now
		}
		if update.ObservedAt.IsZero() {
			update.ObservedAt = update.ReceivedAt
		}
		normalized = append(normalized, update)
	}

	m.mu.Lock()
	for _, update := range normalized {
		current, exists := m.prices[update.Asset]
		if exists && current.ReceivedAt.After(update.ReceivedAt) {
			continue
		}
		m.prices[update.Asset] = update
	}
	changed := m.revalueLocked(now)
	m.mu.Unlock()
	m.publishValuationEvents(changed, now)
	return nil
}

// Revalue makes price expiry observable even when a stream stops producing
// messages. It emits at most one transition per affected cached account.
func (m *Manager) Revalue(now time.Time) {
	if now.IsZero() {
		now = m.now()
	}
	m.mu.Lock()
	changed := m.revalueLocked(now)
	m.mu.Unlock()
	m.publishValuationEvents(changed, now)
}

func (m *Manager) revalueLocked(now time.Time) []domain.Venue {
	changed := make([]domain.Venue, 0, len(m.snapshots))
	for venue, snapshot := range m.snapshots {
		if !m.applyValuationLocked(&snapshot, now) {
			continue
		}
		m.revision++
		snapshot.Revision = m.revision
		m.snapshots[venue] = snapshot
		changed = append(changed, venue)
	}
	slices.Sort(changed)
	return changed
}

func (m *Manager) applyValuationLocked(snapshot *Snapshot, now time.Time) bool {
	previousTotal := snapshot.LocalMarkedTotalUSD
	previousSubtotal := snapshot.PricedSubtotalUSD
	previousBasis := snapshot.ValuationBasis
	previousCompleteness := snapshot.Completeness
	previousUnpriced := append([]string(nil), snapshot.UnpricedAssets...)
	previousAssets := make([]assetValuation, len(snapshot.Assets))
	for index := range snapshot.Assets {
		previousAssets[index] = valuationOf(snapshot.Assets[index])
	}

	subtotal := new(big.Rat)
	priced := false
	complete := len(snapshot.Assets) > 0
	unpriced := make([]string, 0)
	for index := range snapshot.Assets {
		asset := &snapshot.Assets[index]
		quantity, quantityOK := nonNegativeRat(asset.ValuationQuantity)
		if !quantityOK {
			clearLocalValuation(asset)
			unpriced = append(unpriced, asset.Asset)
			complete = false
			continue
		}
		if quantity.Sign() == 0 {
			asset.USDValue = "0"
			asset.PriceSource = "zero valuation quantity"
			asset.PriceAsOf = snapshot.SnapshotAsOf
			asset.Quality = "fresh"
			priced = true
			continue
		}

		price, ok := m.prices[strings.ToUpper(asset.Asset)]
		if !ok || price.ReceivedAt.IsZero() || now.Sub(price.ReceivedAt) > m.valuationMaxAge {
			clearLocalValuation(asset)
			unpriced = append(unpriced, asset.Asset)
			complete = false
			continue
		}
		priceValue, ok := rat(price.Value)
		if !ok || priceValue.Sign() <= 0 {
			clearLocalValuation(asset)
			unpriced = append(unpriced, asset.Asset)
			complete = false
			continue
		}
		marked := new(big.Rat).Mul(quantity, priceValue)
		asset.USDValue = decimal(marked)
		asset.PriceSource = price.Source
		asset.PriceAsOf = price.ObservedAt
		asset.Quality = "fresh"
		subtotal.Add(subtotal, marked)
		priced = true
	}

	snapshot.UnpricedAssets = unpriced
	snapshot.LocalMarkedTotalUSD = ""
	snapshot.PricedSubtotalUSD = ""
	snapshot.ValuationBasis = "public USD mark prices"
	switch {
	case complete:
		snapshot.Completeness = "complete"
		snapshot.PricedSubtotalUSD = decimal(subtotal)
		snapshot.LocalMarkedTotalUSD = decimal(subtotal)
	case priced:
		snapshot.Completeness = "partial"
		snapshot.PricedSubtotalUSD = decimal(subtotal)
	default:
		snapshot.Completeness = "unpriced"
	}

	if previousTotal != snapshot.LocalMarkedTotalUSD || previousSubtotal != snapshot.PricedSubtotalUSD || previousBasis != snapshot.ValuationBasis || previousCompleteness != snapshot.Completeness || !slices.Equal(previousUnpriced, snapshot.UnpricedAssets) {
		return true
	}
	for index := range snapshot.Assets {
		if previousAssets[index] != valuationOf(snapshot.Assets[index]) {
			return true
		}
	}
	return false
}

type assetValuation struct {
	USDValue  string
	Source    string
	PriceAsOf time.Time
	Quality   string
}

func valuationOf(asset Asset) assetValuation {
	return assetValuation{USDValue: asset.USDValue, Source: asset.PriceSource, PriceAsOf: asset.PriceAsOf, Quality: asset.Quality}
}

func clearLocalValuation(asset *Asset) {
	asset.USDValue = ""
	asset.PriceSource = ""
	asset.PriceAsOf = time.Time{}
	asset.Quality = "unpriced"
}

func (m *Manager) publishValuationEvents(venues []domain.Venue, at time.Time) {
	if m.OnEvent == nil {
		return
	}
	for _, venue := range venues {
		m.OnEvent("valuation.updated", venue, at)
	}
}
