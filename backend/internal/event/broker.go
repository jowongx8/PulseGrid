package event

import (
	"errors"
	"sync"
)

const DefaultSubscriberBufferSize = 32

var (
	ErrClosed       = errors.New("event broker is closed")
	ErrInvalidEvent = errors.New("invalid event")
)

// Broker fans out events to in-process subscribers. It does not retain events
// for replay.
type Broker struct {
	mu                   sync.Mutex
	subscriberBufferSize int
	nextSubscriberID     uint64
	subscribers          map[uint64]chan Event
	closed               bool
}

type Subscription struct {
	broker    *Broker
	id        uint64
	events    <-chan Event
	closeOnce sync.Once
}

func NewBroker(subscriberBufferSize int) (*Broker, error) {
	if subscriberBufferSize <= 0 {
		return nil, errors.New("subscriber buffer size must be positive")
	}

	return &Broker{
		subscriberBufferSize: subscriberBufferSize,
		subscribers:          make(map[uint64]chan Event),
	}, nil
}

func (b *Broker) Subscribe() (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, ErrClosed
	}

	id := b.nextSubscriberID
	b.nextSubscriberID++
	events := make(chan Event, b.subscriberBufferSize)
	b.subscribers[id] = events

	return &Subscription{
		broker: b,
		id:     id,
		events: events,
	}, nil
}

func (b *Broker) Publish(value Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}
	if !validValueEvent(value) {
		return ErrInvalidEvent
	}

	for id, events := range b.subscribers {
		select {
		case events <- value:
		default:
			delete(b.subscribers, id)
			close(events)
		}
	}

	return nil
}

func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for id, events := range b.subscribers {
		delete(b.subscribers, id)
		close(events)
	}
}

func (s *Subscription) Events() <-chan Event {
	return s.events
}

func (s *Subscription) Close() {
	s.closeOnce.Do(func() {
		s.broker.unsubscribe(s.id)
	})
}

func (b *Broker) unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	events, exists := b.subscribers[id]
	if !exists {
		return
	}

	delete(b.subscribers, id)
	close(events)
}

func validValueEvent(value Event) bool {
	switch value.(type) {
	case StatusChanged, IncidentOpened, IncidentResolved:
		return true
	default:
		return false
	}
}
