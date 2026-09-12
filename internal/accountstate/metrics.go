package accountstate

import (
	"strings"
	"time"

	"github.com/herefindalex/venuewire/internal/domain"
)

type orderTiming struct {
	AckAt         time.Time
	OrderObserved bool
	ExecObserved  bool
}

type pendingOrderTiming struct {
	OrderAt time.Time
	ExecAt  time.Time
}

func (m *Manager) RecordOrderSubmission(venue domain.Venue, clientOrderID, venueOrderID string, ackAt time.Time, requestRTT time.Duration, accepted, rateLimited bool) {
	if ackAt.IsZero() {
		ackAt = m.now()
	}
	if requestRTT < 0 {
		requestRTT = 0
	}
	m.mu.Lock()
	health, exists := m.health[venue]
	if !exists {
		m.mu.Unlock()
		return
	}
	health.OrderRequestRTT = requestRTT
	if rateLimited {
		health.RateLimitState = "LIMITED"
	} else if accepted {
		health.RateLimitState = "OK"
	}
	if !accepted {
		health.OrderRequestErrors++
	}
	m.health[venue] = health
	if accepted {
		timing := &orderTiming{AckAt: ackAt}
		if key := timingKey("client", clientOrderID); key != "" {
			m.orderTimings[venue][key] = timing
		}
		if key := timingKey("venue", venueOrderID); key != "" {
			m.orderTimings[venue][key] = timing
		}
		m.applyPendingOrderEventsLocked(venue, clientOrderID, venueOrderID, timing, &health)
		m.health[venue] = health
		m.pruneOrderTimingsLocked(venue, ackAt.Add(-time.Hour))
	}
	m.mu.Unlock()
	if m.OnEvent != nil {
		m.OnEvent("venue.health.updated", venue, ackAt)
	}
}

func (m *Manager) RecordOrderEvent(venue domain.Venue, clientOrderID, venueOrderID, kind string, receivedAt time.Time) {
	if receivedAt.IsZero() {
		receivedAt = m.now()
	}
	m.mu.Lock()
	timing := m.orderTimings[venue][timingKey("client", clientOrderID)]
	if timing == nil {
		timing = m.orderTimings[venue][timingKey("venue", venueOrderID)]
	}
	health, exists := m.health[venue]
	changed := false
	if exists && timing == nil {
		m.prunePendingOrderEventsLocked(venue, receivedAt.Add(-5*time.Minute))
		clientKey := timingKey("client", clientOrderID)
		venueKey := timingKey("venue", venueOrderID)
		pending := m.pendingOrderEvents[venue][clientKey]
		if pending == nil {
			pending = m.pendingOrderEvents[venue][venueKey]
		}
		if pending == nil {
			pending = &pendingOrderTiming{}
		}
		if clientKey != "" {
			m.pendingOrderEvents[venue][clientKey] = pending
		}
		if venueKey != "" {
			m.pendingOrderEvents[venue][venueKey] = pending
		}
		recordPendingEvent(pending, kind, receivedAt)
	} else if exists {
		latency := receivedAt.Sub(timing.AckAt)
		if latency < 0 {
			latency = 0
		}
		switch strings.ToLower(strings.TrimSpace(kind)) {
		case "order":
			if !timing.OrderObserved {
				timing.OrderObserved = true
				health.FirstOrderEventLatency = latency
				health.HasFirstOrderEvent = true
				changed = true
			}
		case "execution":
			if !timing.ExecObserved {
				timing.ExecObserved = true
				health.FirstExecutionEventLatency = latency
				health.HasFirstExecutionEvent = true
				changed = true
			}
		}
		m.health[venue] = health
	}
	m.mu.Unlock()
	if changed && m.OnEvent != nil {
		m.OnEvent("venue.health.updated", venue, receivedAt)
	}
}

func (m *Manager) applyPendingOrderEventsLocked(venue domain.Venue, clientOrderID, venueOrderID string, timing *orderTiming, health *VenueHealth) {
	pending := m.pendingOrderEvents[venue][timingKey("client", clientOrderID)]
	if pending == nil {
		pending = m.pendingOrderEvents[venue][timingKey("venue", venueOrderID)]
	}
	delete(m.pendingOrderEvents[venue], timingKey("client", clientOrderID))
	delete(m.pendingOrderEvents[venue], timingKey("venue", venueOrderID))
	if pending == nil {
		return
	}
	if !pending.OrderAt.IsZero() {
		timing.OrderObserved = true
		health.FirstOrderEventLatency = nonNegativeDuration(pending.OrderAt.Sub(timing.AckAt))
		health.HasFirstOrderEvent = true
	}
	if !pending.ExecAt.IsZero() {
		timing.ExecObserved = true
		health.FirstExecutionEventLatency = nonNegativeDuration(pending.ExecAt.Sub(timing.AckAt))
		health.HasFirstExecutionEvent = true
	}
}

func recordPendingEvent(pending *pendingOrderTiming, kind string, at time.Time) {
	if pending == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "order":
		if pending.OrderAt.IsZero() || at.Before(pending.OrderAt) {
			pending.OrderAt = at
		}
	case "execution":
		if pending.ExecAt.IsZero() || at.Before(pending.ExecAt) {
			pending.ExecAt = at
		}
	}
}

func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

func (m *Manager) UpdateReconciliation(venue domain.Venue, status string, discrepancy bool, at time.Time) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		status = "IDLE"
	}
	if at.IsZero() {
		at = m.now()
	}
	m.mu.Lock()
	health, exists := m.health[venue]
	changed := false
	if exists {
		changed = health.ReconciliationStatus != status || discrepancy
		health.ReconciliationStatus = status
		health.LastReconcileAt = at
		if discrepancy {
			health.Discrepancies++
		}
		m.health[venue] = health
	}
	m.mu.Unlock()
	if exists && changed && m.OnEvent != nil {
		m.OnEvent("venue.health.updated", venue, at)
	}
}

func (m *Manager) pruneOrderTimingsLocked(venue domain.Venue, before time.Time) {
	for key, timing := range m.orderTimings[venue] {
		if timing.AckAt.Before(before) {
			delete(m.orderTimings[venue], key)
		}
	}
}

func (m *Manager) prunePendingOrderEventsLocked(venue domain.Venue, before time.Time) {
	for key, pending := range m.pendingOrderEvents[venue] {
		latest := pending.OrderAt
		if pending.ExecAt.After(latest) {
			latest = pending.ExecAt
		}
		if !latest.IsZero() && latest.Before(before) {
			delete(m.pendingOrderEvents[venue], key)
		}
	}
}

func timingKey(kind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return kind + ":" + value
}
