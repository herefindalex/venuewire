package accountstate

import (
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/domain"
)

func TestOrderAndStreamMetricsTrackFirstEvents(t *testing.T) {
	manager, err := NewManager([]Provider{&fakeProvider{venue: domain.VenueBybit}}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ackAt := time.Date(2026, 9, 10, 16, 0, 0, 0, time.UTC)
	manager.RecordOrderSubmission(domain.VenueBybit, "client-1", "venue-1", ackAt, 82*time.Millisecond, true, false)
	manager.RecordOrderEvent(domain.VenueBybit, "client-1", "", "order", ackAt.Add(104*time.Millisecond))
	manager.RecordOrderEvent(domain.VenueBybit, "", "venue-1", "order", ackAt.Add(200*time.Millisecond))
	manager.RecordOrderEvent(domain.VenueBybit, "", "venue-1", "execution", ackAt.Add(116*time.Millisecond))
	manager.UpdateWSReceiveTimes(domain.VenueBybit, ackAt.Add(time.Second), ackAt.Add(2*time.Second))
	manager.UpdateWSReceiveTimes(domain.VenueBybit, ackAt, ackAt)

	health := manager.Health()[0]
	if health.OrderRequestRTT != 82*time.Millisecond || !health.HasFirstOrderEvent || health.FirstOrderEventLatency != 104*time.Millisecond || !health.HasFirstExecutionEvent || health.FirstExecutionEventLatency != 116*time.Millisecond {
		t.Fatalf("order metrics = %+v", health)
	}
	if health.RateLimitState != "OK" || !health.LastPublicReceiveAt.Equal(ackAt.Add(time.Second)) || !health.LastPrivateReceiveAt.Equal(ackAt.Add(2*time.Second)) {
		t.Fatalf("runtime metrics = %+v", health)
	}

	manager.RecordOrderSubmission(domain.VenueBybit, "client-2", "", ackAt.Add(time.Minute), 20*time.Millisecond, false, true)
	health = manager.Health()[0]
	if health.RateLimitState != "LIMITED" || health.OrderRequestErrors != 1 {
		t.Fatalf("rate limit metrics = %+v", health)
	}
}

func TestReconciliationMetricsCountUnknownResolutionOnce(t *testing.T) {
	manager, err := NewManager([]Provider{&fakeProvider{venue: domain.VenueDeribit}}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 10, 17, 0, 0, 0, time.UTC)
	manager.UpdateReconciliation(domain.VenueDeribit, "RUNNING", false, now)
	manager.UpdateReconciliation(domain.VenueDeribit, "SYNCED", true, now.Add(time.Second))
	health := manager.Health()[0]
	if health.ReconciliationStatus != "SYNCED" || health.Discrepancies != 1 || !health.LastReconcileAt.Equal(now.Add(time.Second)) {
		t.Fatalf("reconciliation metrics = %+v", health)
	}
}

func TestPrivateEventBeforeAckIsCorrelatedAfterSubmissionReturns(t *testing.T) {
	manager, err := NewManager([]Provider{&fakeProvider{venue: domain.VenueDeribit}}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ackAt := time.Date(2026, 9, 10, 18, 0, 0, 100, time.UTC)
	manager.RecordOrderEvent(domain.VenueDeribit, "client-early", "venue-early", "order", ackAt.Add(-time.Millisecond))
	manager.RecordOrderEvent(domain.VenueDeribit, "", "venue-early", "execution", ackAt.Add(-500*time.Microsecond))
	manager.RecordOrderSubmission(domain.VenueDeribit, "client-early", "venue-early", ackAt, 10*time.Millisecond, true, false)
	health := manager.Health()[0]
	if !health.HasFirstOrderEvent || health.FirstOrderEventLatency != 0 || !health.HasFirstExecutionEvent || health.FirstExecutionEventLatency != 0 {
		t.Fatalf("early private events were not correlated: %+v", health)
	}
}
