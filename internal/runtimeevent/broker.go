package runtimeevent

import (
	"sync"
	"time"

	"github.com/herefindalex/venuewire/internal/domain"
)

type Event struct {
	Sequence uint64       `json:"sequence"`
	Type     string       `json:"type"`
	Venue    domain.Venue `json:"venue,omitempty"`
	IntentID string       `json:"intentId,omitempty"`
	At       time.Time    `json:"at"`
}

type Broker struct {
	mu          sync.Mutex
	subscribers map[uint64]chan Event
	nextID      uint64
	sequence    uint64
}

func NewBroker() *Broker {
	return &Broker{subscribers: make(map[uint64]chan Event)}
}

func (b *Broker) Publish(event Event) {
	if b == nil || event.Type == "" {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.sequence++
	event.Sequence = b.sequence
	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
			// A slow Browser client must never block an exchange stream. Replace
			// queued deltas with an explicit resync marker.
			for {
				select {
				case <-subscriber:
				default:
					goto drained
				}
			}
		drained:
			select {
			case subscriber <- Event{Sequence: event.Sequence, Type: "resync.required", At: event.At}:
			default:
			}
		}
	}
}

func (b *Broker) Subscribe(buffer int) (<-chan Event, func()) {
	events, _, unsubscribe := b.SubscribeFrom(buffer)
	return events, unsubscribe
}

// SubscribeFrom atomically registers a subscriber and returns the last event
// sequence that existed before registration. Callers can build an initial
// snapshot and then consume only events newer than this cursor without an
// event-registration gap.
func (b *Broker) SubscribeFrom(buffer int) (<-chan Event, uint64, func()) {
	if buffer < 1 {
		buffer = 1
	}
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	events := make(chan Event, buffer)
	b.subscribers[id] = events
	cursor := b.sequence
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, id)
			b.mu.Unlock()
		})
	}
	return events, cursor, unsubscribe
}

func (b *Broker) Sequence() uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sequence
}
