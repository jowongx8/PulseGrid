package event

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/status"
)

const brokerTestTimeout = 5 * time.Second

type unsupportedEvent struct{}

func (unsupportedEvent) event() {}

func TestNewBrokerRequiresPositiveSubscriberBufferSize(t *testing.T) {
	for _, size := range []int{-1, 0} {
		if _, err := NewBroker(size); err == nil {
			t.Fatalf("NewBroker(%d) error = nil, want error", size)
		}
	}
}

func TestBrokerDeliversToSingleSubscriber(t *testing.T) {
	broker := newTestBroker(t, 1)
	subscription := subscribeTestBroker(t, broker)
	want := StatusChanged{
		ServiceID:     "github",
		State:         status.StatusDown,
		LastCheckedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
	}

	if err := broker.Publish(want); err != nil {
		t.Fatal(err)
	}
	assertEvent(t, receiveEvent(t, subscription.Events()), want)
}

func TestBrokerFansOutToMultipleSubscribers(t *testing.T) {
	broker := newTestBroker(t, 1)
	first := subscribeTestBroker(t, broker)
	second := subscribeTestBroker(t, broker)
	want := IncidentOpened{
		ServiceID: "github",
		StartedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
	}

	if err := broker.Publish(want); err != nil {
		t.Fatal(err)
	}
	assertEvent(t, receiveEvent(t, first.Events()), want)
	assertEvent(t, receiveEvent(t, second.Events()), want)
}

func TestBrokerPreservesSequentialPublicationOrder(t *testing.T) {
	broker := newTestBroker(t, 3)
	subscription := subscribeTestBroker(t, broker)
	startedAt := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	want := []Event{
		StatusChanged{ServiceID: "github", State: status.StatusDown, LastCheckedAt: startedAt},
		IncidentOpened{ServiceID: "github", StartedAt: startedAt},
		IncidentResolved{ServiceID: "github", StartedAt: startedAt, ResolvedAt: startedAt.Add(time.Minute)},
	}

	for _, value := range want {
		if err := broker.Publish(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range want {
		assertEvent(t, receiveEvent(t, subscription.Events()), value)
	}
}

func TestBrokerPublishesIndependentEventValues(t *testing.T) {
	broker := newTestBroker(t, 1)
	first := subscribeTestBroker(t, broker)
	second := subscribeTestBroker(t, broker)
	original := StatusChanged{
		ServiceID:     "github",
		State:         status.StatusDown,
		LastCheckedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
	}
	want := original

	if err := broker.Publish(original); err != nil {
		t.Fatal(err)
	}
	original.ServiceID = "changed"
	original.State = status.StatusUp
	original.LastCheckedAt = original.LastCheckedAt.Add(time.Hour)

	firstValue, ok := receiveEvent(t, first.Events()).(StatusChanged)
	if !ok {
		t.Fatal("first subscriber did not receive StatusChanged")
	}
	if firstValue != want {
		t.Fatalf("first event = %+v, want %+v", firstValue, want)
	}
	firstValue.ServiceID = "mutated-by-subscriber"

	secondValue, ok := receiveEvent(t, second.Events()).(StatusChanged)
	if !ok {
		t.Fatal("second subscriber did not receive StatusChanged")
	}
	if secondValue != want {
		t.Fatalf("second event = %+v, want %+v", secondValue, want)
	}
}

func TestBrokerRejectsInvalidEventForms(t *testing.T) {
	broker := newTestBroker(t, 1)
	subscription := subscribeTestBroker(t, broker)
	value := StatusChanged{
		ServiceID:     "github",
		State:         status.StatusUp,
		LastCheckedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
	}
	var nilPointer *StatusChanged

	for name, invalid := range map[string]Event{
		"nil interface":        nil,
		"value pointer":        &value,
		"typed nil pointer":    nilPointer,
		"unsupported concrete": unsupportedEvent{},
	} {
		t.Run(name, func(t *testing.T) {
			if err := broker.Publish(invalid); !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("Publish() error = %v, want ErrInvalidEvent", err)
			}
			if got := len(subscription.Events()); got != 0 {
				t.Fatalf("subscriber queue length = %d, want 0", got)
			}
		})
	}
}

func TestSubscriptionCloseIsIdempotent(t *testing.T) {
	broker := newTestBroker(t, 1)
	subscription := subscribeTestBroker(t, broker)

	subscription.Close()
	subscription.Close()
	assertChannelClosed(t, subscription.Events())

	if err := broker.Publish(IncidentOpened{ServiceID: "github", StartedAt: time.Now()}); err != nil {
		t.Fatalf("Publish() after unsubscribe error = %v", err)
	}
}

func TestBrokerDisconnectsSlowSubscriber(t *testing.T) {
	broker := newTestBroker(t, 1)
	subscription := subscribeTestBroker(t, broker)
	first := IncidentOpened{ServiceID: "github", StartedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)}
	second := StatusChanged{ServiceID: "github", State: status.StatusDown, LastCheckedAt: first.StartedAt}

	if err := broker.Publish(first); err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(second); err != nil {
		t.Fatal(err)
	}

	assertEvent(t, receiveEvent(t, subscription.Events()), first)
	assertChannelClosed(t, subscription.Events())
	subscription.Close()
}

func TestSlowSubscriberDoesNotAffectHealthySubscriber(t *testing.T) {
	broker := newTestBroker(t, 1)
	slow := subscribeTestBroker(t, broker)
	healthy := subscribeTestBroker(t, broker)
	first := IncidentOpened{ServiceID: "github", StartedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)}
	second := StatusChanged{ServiceID: "github", State: status.StatusDown, LastCheckedAt: first.StartedAt}
	third := IncidentResolved{ServiceID: "github", StartedAt: first.StartedAt, ResolvedAt: first.StartedAt.Add(time.Minute)}

	if err := broker.Publish(first); err != nil {
		t.Fatal(err)
	}
	assertEvent(t, receiveEvent(t, healthy.Events()), first)

	if err := broker.Publish(second); err != nil {
		t.Fatal(err)
	}
	assertEvent(t, receiveEvent(t, healthy.Events()), second)
	assertEvent(t, receiveEvent(t, slow.Events()), first)
	assertChannelClosed(t, slow.Events())

	if err := broker.Publish(third); err != nil {
		t.Fatal(err)
	}
	assertEvent(t, receiveEvent(t, healthy.Events()), third)
}

func TestBrokerCloseIsIdempotentAndClosesSubscribers(t *testing.T) {
	broker := newTestBroker(t, 1)
	first := subscribeTestBroker(t, broker)
	second := subscribeTestBroker(t, broker)

	broker.Close()
	broker.Close()
	assertChannelClosed(t, first.Events())
	assertChannelClosed(t, second.Events())
	first.Close()
	second.Close()
}

func TestBrokerRejectsOperationsAfterClose(t *testing.T) {
	broker := newTestBroker(t, 1)
	broker.Close()

	if err := broker.Publish(IncidentOpened{ServiceID: "github", StartedAt: time.Now()}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Publish() error = %v, want ErrClosed", err)
	}
	if subscription, err := broker.Subscribe(); subscription != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe() = (%v, %v), want (nil, ErrClosed)", subscription, err)
	}
}

func TestBrokerConcurrentLifecycleOperations(t *testing.T) {
	const iterations = 100

	for iteration := range iterations {
		broker, err := NewBroker(1)
		if err != nil {
			t.Fatal(err)
		}
		existing, err := broker.Subscribe()
		if err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		publishResult := make(chan error, 1)
		type subscribeResult struct {
			subscription *Subscription
			err          error
		}
		subscribeResults := make(chan subscribeResult, 1)
		var operations sync.WaitGroup
		operations.Add(4)

		go func() {
			defer operations.Done()
			<-start
			publishResult <- broker.Publish(IncidentOpened{
				ServiceID: "github",
				StartedAt: time.Date(2026, 9, 22, 10, 0, iteration, 0, time.UTC),
			})
		}()
		go func() {
			defer operations.Done()
			<-start
			existing.Close()
		}()
		go func() {
			defer operations.Done()
			<-start
			broker.Close()
		}()
		go func() {
			defer operations.Done()
			<-start
			subscription, err := broker.Subscribe()
			subscribeResults <- subscribeResult{subscription: subscription, err: err}
		}()

		close(start)
		operations.Wait()

		if err := <-publishResult; err != nil && !errors.Is(err, ErrClosed) {
			t.Fatalf("iteration %d: Publish() error = %v", iteration, err)
		}
		result := <-subscribeResults
		switch {
		case result.err == nil:
			result.subscription.Close()
			assertChannelDrainsAndCloses(t, result.subscription.Events())
		case errors.Is(result.err, ErrClosed) && result.subscription == nil:
		default:
			t.Fatalf("iteration %d: Subscribe() = (%v, %v)", iteration, result.subscription, result.err)
		}

		existing.Close()
		broker.Close()
		assertChannelDrainsAndCloses(t, existing.Events())
	}
}

func newTestBroker(t *testing.T, bufferSize int) *Broker {
	t.Helper()
	broker, err := NewBroker(bufferSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	return broker
}

func subscribeTestBroker(t *testing.T, broker *Broker) *Subscription {
	t.Helper()
	subscription, err := broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	return subscription
}

func receiveEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case value, ok := <-events:
		if !ok {
			t.Fatal("event channel closed before delivery")
		}
		return value
	case <-time.After(brokerTestTimeout):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

func assertEvent(t *testing.T, got Event, want Event) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event = %#v, want %#v", got, want)
	}
}

func assertChannelClosed(t *testing.T, events <-chan Event) {
	t.Helper()
	select {
	case value, ok := <-events:
		if ok {
			t.Fatalf("received unexpected event %#v before channel closure", value)
		}
	case <-time.After(brokerTestTimeout):
		t.Fatal("timed out waiting for event channel to close")
	}
}

func assertChannelDrainsAndCloses(t *testing.T, events <-chan Event) {
	t.Helper()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-time.After(brokerTestTimeout):
			t.Fatal("timed out waiting for event channel to drain and close")
		}
	}
}
