package runtimeevent

import (
	"sync"
	"testing"
	"time"
)

func TestBrokerPublishesOrderedEventsAndUnsubscribes(t *testing.T) {
	broker := NewBroker()
	events, unsubscribe := broker.Subscribe(2)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	broker.Publish(Event{Type: "account.updated", At: now})
	broker.Publish(Event{Type: "trade.updated", At: now})
	first, second := <-events, <-events
	if first.Sequence != 1 || second.Sequence != 2 || first.Type != "account.updated" || second.Type != "trade.updated" {
		t.Fatalf("events = %+v %+v", first, second)
	}
	unsubscribe()
	unsubscribe()
}

func TestSlowSubscriberReceivesResyncInsteadOfBlockingPublisher(t *testing.T) {
	broker := NewBroker()
	events, unsubscribe := broker.Subscribe(1)
	defer unsubscribe()
	broker.Publish(Event{Type: "one"})
	broker.Publish(Event{Type: "two"})
	event := <-events
	if event.Type != "resync.required" || event.Sequence != 2 {
		t.Fatalf("overflow event = %+v", event)
	}
}

func TestSubscribeFromReturnsAtomicCursor(t *testing.T) {
	broker := NewBroker()
	broker.Publish(Event{Type: "before"})
	events, cursor, unsubscribe := broker.SubscribeFrom(2)
	defer unsubscribe()
	if cursor != 1 || broker.Sequence() != 1 {
		t.Fatalf("cursor/sequence = %d/%d, want 1/1", cursor, broker.Sequence())
	}
	broker.Publish(Event{Type: "after"})
	event := <-events
	if event.Sequence != 2 || event.Type != "after" {
		t.Fatalf("event = %+v", event)
	}
}

func TestConcurrentPublishSequenceMatchesDeliveryOrder(t *testing.T) {
	broker := NewBroker()
	events, unsubscribe := broker.Subscribe(64)
	defer unsubscribe()
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			broker.Publish(Event{Type: "concurrent"})
		}()
	}
	wg.Wait()
	for want := uint64(1); want <= 32; want++ {
		if event := <-events; event.Sequence != want {
			t.Fatalf("event sequence = %d, want %d", event.Sequence, want)
		}
	}
}
