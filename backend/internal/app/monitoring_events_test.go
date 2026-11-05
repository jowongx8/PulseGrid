package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/event"
	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
)

func TestProcessResultsDoesNotPublishWithoutStatusChange(t *testing.T) {
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		setup  func(*status.Tracker)
		result monitoring.CheckResult
	}{
		{
			name: "unchanged",
			setup: func(tracker *status.Tracker) {
				applyTestObservation(t, tracker, monitoring.ObservationHealthy, base)
			},
			result: monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(time.Second), StatusCode: 200},
		},
		{
			name:   "ignored",
			setup:  func(*status.Tracker) {},
			result: monitoring.CheckResult{ServiceID: "example", CheckedAt: base, ErrorKind: monitoring.ErrorCanceled},
		},
		{
			name: "stale",
			setup: func(tracker *status.Tracker) {
				applyTestObservation(t, tracker, monitoring.ObservationHealthy, base.Add(time.Second))
			},
			result: monitoring.CheckResult{ServiceID: "example", CheckedAt: base, StatusCode: 503},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTestTracker(t)
			tt.setup(tracker)
			var published []event.Event
			guard := outstandingGuard(tt.result.ServiceID)
			err := processSingleResult(
				t, tt.result, tracker, newTestIncidentProcessor(),
				eventPublisherFunc(func(value event.Event) error {
					published = append(published, value)
					return nil
				}),
				guard,
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(published) != 0 {
				t.Fatalf("published events = %#v, want none", published)
			}
			assertNotOutstanding(t, guard, tt.result.ServiceID)
		})
	}
}

func TestProcessResultsPublishesStatusChangeOnly(t *testing.T) {
	checkedAt := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	tracker := newTestTracker(t)
	guard := outstandingGuard("example")
	var published []event.Event

	err := processSingleResult(
		t,
		monitoring.CheckResult{ServiceID: "example", CheckedAt: checkedAt, StatusCode: 200},
		tracker,
		newTestIncidentProcessor(),
		eventPublisherFunc(func(value event.Event) error {
			published = append(published, value)
			return nil
		}),
		guard,
	)
	if err != nil {
		t.Fatal(err)
	}

	want := []event.Event{event.StatusChanged{
		ServiceID:     "example",
		State:         status.StatusUp,
		LastCheckedAt: checkedAt,
	}}
	if !reflect.DeepEqual(published, want) {
		t.Fatalf("published events = %#v, want %#v", published, want)
	}
	assertNotOutstanding(t, guard, "example")
}

func TestProcessResultsPublishesOpeningAfterPersistence(t *testing.T) {
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	startedAt := base.Add(2*time.Second + 123_456_789*time.Nanosecond)
	canonicalStartedAt := time.UnixMicro(startedAt.UnixMicro()).UTC()
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 2)
	persisted := false
	processor := NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{
		open: func(_ context.Context, value incident.Incident) error {
			persisted = true
			if value.ServiceID != "example" || value.StartedAt != canonicalStartedAt || value.ResolvedAt != nil {
				return errors.New("unexpected opening persisted")
			}
			return nil
		},
	})
	guard := outstandingGuard("example")
	var published []event.Event
	publishedBeforePersistence := false

	err := processSingleResult(
		t,
		monitoring.CheckResult{ServiceID: "example", CheckedAt: startedAt, StatusCode: 503},
		tracker,
		processor,
		eventPublisherFunc(func(value event.Event) error {
			if !persisted {
				publishedBeforePersistence = true
			}
			published = append(published, value)
			return nil
		}),
		guard,
	)
	if err != nil {
		t.Fatal(err)
	}

	want := []event.Event{
		event.StatusChanged{ServiceID: "example", State: status.StatusDown, LastCheckedAt: startedAt},
		event.IncidentOpened{ServiceID: "example", StartedAt: canonicalStartedAt},
	}
	if publishedBeforePersistence || !reflect.DeepEqual(published, want) {
		t.Fatalf("published before persistence = %t, events = %#v, want %#v", publishedBeforePersistence, published, want)
	}
	assertNotOutstanding(t, guard, "example")
}

func TestProcessResultsPublishesResolutionAfterPersistence(t *testing.T) {
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	startedAt := base.Add(2*time.Second + 123_456_789*time.Nanosecond)
	resolvedAt := base.Add(4*time.Second + 987_654_321*time.Nanosecond)
	canonicalStartedAt := time.UnixMicro(startedAt.UnixMicro()).UTC()
	canonicalResolvedAt := time.UnixMicro(resolvedAt.UnixMicro()).UTC()
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 3)
	applyTestObservation(t, tracker, monitoring.ObservationHealthy, base.Add(3*time.Second))
	lifecycle := incident.NewLifecycle()
	if _, err := lifecycle.Apply(testIncidentUpdate(status.StatusUnknown, status.StatusDown, startedAt)); err != nil {
		t.Fatal(err)
	}
	persisted := false
	processor := NewIncidentProcessor(lifecycle, incidentWriterFuncs{
		resolve: func(_ context.Context, value incident.Incident) error {
			persisted = true
			if value.ResolvedAt == nil || value.StartedAt != canonicalStartedAt || *value.ResolvedAt != canonicalResolvedAt {
				return errors.New("unexpected resolution persisted")
			}
			return nil
		},
	})
	guard := outstandingGuard("example")
	var published []event.Event
	publishedBeforePersistence := false

	err := processSingleResult(
		t,
		monitoring.CheckResult{ServiceID: "example", CheckedAt: resolvedAt, StatusCode: 200},
		tracker,
		processor,
		eventPublisherFunc(func(value event.Event) error {
			if !persisted {
				publishedBeforePersistence = true
			}
			published = append(published, value)
			return nil
		}),
		guard,
	)
	if err != nil {
		t.Fatal(err)
	}

	want := []event.Event{
		event.StatusChanged{ServiceID: "example", State: status.StatusUp, LastCheckedAt: resolvedAt},
		event.IncidentResolved{ServiceID: "example", StartedAt: canonicalStartedAt, ResolvedAt: canonicalResolvedAt},
	}
	if publishedBeforePersistence || !reflect.DeepEqual(published, want) {
		t.Fatalf("published before persistence = %t, events = %#v, want %#v", publishedBeforePersistence, published, want)
	}
	assertNotOutstanding(t, guard, "example")
}

func TestProcessResultsPublishesUnknownWhileKeepingIncidentOpen(t *testing.T) {
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	startedAt := base.Add(2 * time.Second)
	unknownAt := base.Add(5 * time.Second)
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 3)
	applyTestObservation(t, tracker, monitoring.ObservationIndeterminate, base.Add(3*time.Second))
	applyTestObservation(t, tracker, monitoring.ObservationIndeterminate, base.Add(4*time.Second))
	lifecycle := incident.NewLifecycle()
	if _, err := lifecycle.Apply(testIncidentUpdate(status.StatusUnknown, status.StatusDown, startedAt)); err != nil {
		t.Fatal(err)
	}
	processor := NewIncidentProcessor(lifecycle, incidentWriterFuncs{
		open:    func(context.Context, incident.Incident) error { return errors.New("unexpected open") },
		resolve: func(context.Context, incident.Incident) error { return errors.New("unexpected resolve") },
	})
	guard := outstandingGuard("example")
	var published []event.Event

	err := processSingleResult(
		t,
		monitoring.CheckResult{ServiceID: "example", CheckedAt: unknownAt, StatusCode: 404},
		tracker,
		processor,
		eventPublisherFunc(func(value event.Event) error {
			published = append(published, value)
			return nil
		}),
		guard,
	)
	if err != nil {
		t.Fatal(err)
	}

	want := []event.Event{event.StatusChanged{ServiceID: "example", State: status.StatusUnknown, LastCheckedAt: unknownAt}}
	if !reflect.DeepEqual(published, want) {
		t.Fatalf("published events = %#v, want %#v", published, want)
	}
	active, ok := lifecycle.Active("example")
	if !ok || !active.StartedAt.Equal(startedAt) || active.ResolvedAt != nil {
		t.Fatalf("active incident = (%+v, %t), want original open incident", active, ok)
	}
	assertNotOutstanding(t, guard, "example")
}

func TestProcessResultsPublishesNothingWhenIncidentPersistenceFails(t *testing.T) {
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 2)
	wantErr := errors.New("incident persistence failed")
	processor := NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{
		open: func(context.Context, incident.Incident) error { return wantErr },
	})
	guard := outstandingGuard("example")
	var published []event.Event

	err := processSingleResult(
		t,
		monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(2 * time.Second), StatusCode: 503},
		tracker,
		processor,
		eventPublisherFunc(func(value event.Event) error {
			published = append(published, value)
			return nil
		}),
		guard,
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("processResults() error = %v, want persistence failure", err)
	}
	if len(published) != 0 {
		t.Fatalf("published events = %#v, want none", published)
	}
	snapshot, snapshotErr := tracker.Get("example")
	if snapshotErr != nil || snapshot.Status != status.StatusDown {
		t.Fatalf("tracker after persistence failure = (%+v, %v), want DOWN", snapshot, snapshotErr)
	}
	assertOutstanding(t, guard, "example")
}

func TestProcessResultsStopsAfterStatusPublicationFailure(t *testing.T) {
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 2)
	persisted := false
	processor := NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{
		open: func(context.Context, incident.Incident) error { persisted = true; return nil },
	})
	guard := outstandingGuard("example")
	wantErr := errors.New("status publish failed")
	var attempted []event.Event

	err := processSingleResult(
		t,
		monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(2 * time.Second), StatusCode: 503},
		tracker,
		processor,
		eventPublisherFunc(func(value event.Event) error {
			attempted = append(attempted, value)
			return wantErr
		}),
		guard,
	)
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "publish status changed event") {
		t.Fatalf("processResults() error = %v, want wrapped status publication failure", err)
	}
	if !persisted || len(attempted) != 1 {
		t.Fatalf("incident persisted = %t, attempted events = %#v, want one status attempt after persistence", persisted, attempted)
	}
	if _, ok := attempted[0].(event.StatusChanged); !ok {
		t.Fatalf("first publication attempt = %T, want StatusChanged", attempted[0])
	}
	assertOutstanding(t, guard, "example")
}

func TestProcessResultsKeepsStatusWhenIncidentPublicationFails(t *testing.T) {
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 2)
	processor := NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{})
	guard := outstandingGuard("example")
	wantErr := errors.New("incident publish failed")
	var attempted, published []event.Event

	err := processSingleResult(
		t,
		monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(2 * time.Second), StatusCode: 503},
		tracker,
		processor,
		eventPublisherFunc(func(value event.Event) error {
			attempted = append(attempted, value)
			if len(attempted) == 2 {
				return wantErr
			}
			published = append(published, value)
			return nil
		}),
		guard,
	)
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "publish incident opened event") {
		t.Fatalf("processResults() error = %v, want wrapped incident publication failure", err)
	}
	if len(attempted) != 2 || len(published) != 1 {
		t.Fatalf("attempted = %#v, published = %#v", attempted, published)
	}
	if _, ok := attempted[0].(event.StatusChanged); !ok {
		t.Fatalf("first publication attempt = %T, want StatusChanged", attempted[0])
	}
	if _, ok := attempted[1].(event.IncidentOpened); !ok {
		t.Fatalf("second publication attempt = %T, want IncidentOpened", attempted[1])
	}
	if _, ok := published[0].(event.StatusChanged); !ok {
		t.Fatalf("successfully published event = %T, want StatusChanged", published[0])
	}
	assertOutstanding(t, guard, "example")
}

func TestMonitoringRuntimePropagatesPublicationFailureAndJoinsWorkers(t *testing.T) {
	startedSibling := make(chan struct{})
	stoppedSibling := make(chan struct{})
	checkedAt := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	checker := checkerFunc(func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		if svc.ID == "sibling" {
			close(startedSibling)
			<-ctx.Done()
			close(stoppedSibling)
			return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: checkedAt, ErrorKind: monitoring.ErrorCanceled}
		}
		<-startedSibling
		return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: checkedAt, StatusCode: 200}
	})
	runtime, err := NewMonitoringRuntime(
		[]service.Service{{ID: "primary", Enabled: true}, {ID: "sibling", Enabled: true}},
		checker,
		checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error { return nil }),
		NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{}),
		eventPublisherFunc(func(event.Event) error { return event.ErrClosed }),
		time.Hour,
		2,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.pool.Submit(context.Background(), service.Service{ID: "sibling", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	if err := waitError(t, done); !errors.Is(err, event.ErrClosed) || !strings.Contains(err.Error(), "result consumer") {
		t.Fatalf("MonitoringRuntime.Run() error = %v, want wrapped ErrClosed", err)
	}
	waitSignal(t, stoppedSibling)
	assertResultsClosed(t, runtime.pool.Results())
}

func processSingleResult(
	t *testing.T,
	result monitoring.CheckResult,
	tracker *status.Tracker,
	processor *IncidentProcessor,
	publisher EventPublisher,
	guard *outstandingSubmitter,
) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan monitoring.CheckResult, 1)
	results <- result
	writer := checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error {
		cancel()
		return nil
	})
	return processResults(ctx, results, writer, tracker, processor, publisher, guard)
}

func applyTestObservation(t *testing.T, tracker *status.Tracker, kind monitoring.ObservationKind, at time.Time) {
	t.Helper()
	if _, err := tracker.Apply(monitoring.Observation{ServiceID: "example", ObservedAt: at, Kind: kind}); err != nil {
		t.Fatal(err)
	}
}

func outstandingGuard(serviceID string) *outstandingSubmitter {
	return &outstandingSubmitter{outstanding: map[string]struct{}{serviceID: {}}}
}

func assertOutstanding(t *testing.T, guard *outstandingSubmitter, serviceID string) {
	t.Helper()
	if _, ok := guard.outstanding[serviceID]; !ok {
		t.Fatalf("service %q is not outstanding", serviceID)
	}
}

func assertNotOutstanding(t *testing.T, guard *outstandingSubmitter, serviceID string) {
	t.Helper()
	if _, ok := guard.outstanding[serviceID]; ok {
		t.Fatalf("service %q remains outstanding", serviceID)
	}
}
