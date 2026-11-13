package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/event"
	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
	"github.com/jowongx8/backend/internal/storage/sqlite"
)

func TestCatalogueResultPipelinePersistsHistoryAndIncidentRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, incidents := openRestartTestStore(t)
	services := service.Catalogue()
	var ids []string
	for _, svc := range services {
		ids = append(ids, svc.ID)
	}
	tracker, err := status.NewTracker(ids)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := event.NewBroker(event.DefaultSubscriberBufferSize)
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	base := time.Date(2026, 11, 12, 12, 0, 0, 123456000, time.UTC)
	results := make(chan monitoring.CheckResult, 4)
	fed := make(chan struct{})
	go func() {
		defer close(fed)
		defer close(results)
		for cycle := range 6 {
			for index, svc := range services {
				code := 200
				if index == 0 && cycle >= 1 && cycle <= 3 {
					code = 503
				}
				select {
				case results <- monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: base.Add(time.Duration(cycle) * 15 * time.Second), Duration: time.Millisecond, StatusCode: code}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-fed }()
	err = processResults(ctx, results, sqlite.NewCheckResultStore(db), tracker, NewIncidentProcessor(incident.NewLifecycle(), incidents), broker, &outstandingSubmitter{outstanding: map[string]struct{}{}})
	if err == nil || !strings.Contains(err.Error(), "results channel closed") {
		t.Fatalf("batch termination = %v, want exhausted input after processing", err)
	}
	rows, err := db.Query("SELECT service_id, COUNT(*) FROM check_results GROUP BY service_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			t.Fatal(err)
		}
		counts[id] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(counts) != len(services) {
		t.Fatalf("persisted membership = %v, want all catalogue services", counts)
	}
	for _, svc := range services {
		if counts[svc.ID] != 6 {
			t.Errorf("%s persisted %d results, want six complete cycles", svc.ID, counts[svc.ID])
		}
	}
	query, err := NewIncidentsQuery(incidents, services)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := query.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Active) != 0 || len(feed.Resolved) != 1 || feed.Resolved[0].ServiceID != services[0].ID ||
		!feed.Resolved[0].StartedAt.Equal(base.Add(45*time.Second)) || feed.Resolved[0].ResolvedAt == nil || !feed.Resolved[0].ResolvedAt.Equal(base.Add(75*time.Second)) {
		t.Fatalf("incident lifecycle under catalogue load = %+v, want one canonical resolution", feed)
	}
	view, err := NewCurrentStatusQuery(services, tracker)
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range view.Execute() {
		if svc.Status.State != status.StatusUp {
			t.Errorf("%s final state = %s, want UP after recovery", svc.ID, svc.Status.State)
		}
	}
}

func TestProcessResultsSavesBeforeTrackerAndReleasesOutstanding(t *testing.T) {
	svc := service.Service{ID: "example", Enabled: true}
	tracker := newTestTracker(t)
	delegateCalls := 0
	guard := &outstandingSubmitter{
		delegate: submitterFunc(func(context.Context, service.Service) error {
			delegateCalls++
			return nil
		}),
		outstanding: make(map[string]struct{}),
	}
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}

	result := monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC), StatusCode: 200}
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var saved monitoring.CheckResult
	var saveCalls atomic.Int32
	writer := checkResultWriterFunc(func(_ context.Context, got monitoring.CheckResult) error {
		saved = got
		saveCalls.Add(1)
		close(started)
		<-release
		return nil
	})
	results := make(chan monitoring.CheckResult, 1)
	results <- result
	close(results)
	done := make(chan error, 1)
	finished := make(chan struct{})
	defer func() {
		unblock()
		waitSignal(t, finished)
	}()
	go func() {
		defer close(finished)
		done <- processResults(context.Background(), results, writer, tracker, newTestIncidentProcessor(), successfulEventPublisher(), guard)
	}()
	waitSignal(t, started)

	snapshot, err := tracker.Get(svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUnknown || !snapshot.LastObservedAt.IsZero() {
		t.Fatalf("tracker changed before Save succeeded: %+v", snapshot)
	}
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if delegateCalls != 1 {
		t.Fatalf("delegate calls during blocked Save = %d, want 1", delegateCalls)
	}

	unblock()
	if err := waitError(t, done); err == nil || !strings.Contains(err.Error(), "results channel closed") {
		t.Fatalf("processResults() error = %v, want active-channel closure", err)
	}
	if got := saveCalls.Load(); got != 1 || saved != result {
		t.Fatalf("Save calls/result = (%d, %+v), want (1, %+v)", got, saved, result)
	}
	snapshot, err = tracker.Get(svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUp || !snapshot.LastObservedAt.Equal(result.CheckedAt) {
		t.Fatalf("tracker after Save = %+v, want UP at %v", snapshot, result.CheckedAt)
	}
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if delegateCalls != 2 {
		t.Fatalf("delegate calls after processing = %d, want 2", delegateCalls)
	}
}

func TestProcessResultsPersistsRawFailures(t *testing.T) {
	base := monitoring.CheckResult{ServiceID: "example", CheckedAt: time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)}
	tests := []struct {
		name   string
		result monitoring.CheckResult
	}{
		{name: "HTTP 404", result: monitoring.CheckResult{ServiceID: base.ServiceID, CheckedAt: base.CheckedAt, StatusCode: 404}},
		{name: "HTTP 503", result: monitoring.CheckResult{ServiceID: base.ServiceID, CheckedAt: base.CheckedAt, StatusCode: 503}},
		{name: "timeout", result: monitoring.CheckResult{ServiceID: base.ServiceID, CheckedAt: base.CheckedAt, ErrorKind: monitoring.ErrorTimeout}},
		{name: "DNS", result: monitoring.CheckResult{ServiceID: base.ServiceID, CheckedAt: base.CheckedAt, ErrorKind: monitoring.ErrorDNS}},
		{name: "connection", result: monitoring.CheckResult{ServiceID: base.ServiceID, CheckedAt: base.CheckedAt, ErrorKind: monitoring.ErrorConnection}},
		{name: "TLS", result: monitoring.CheckResult{ServiceID: base.ServiceID, CheckedAt: base.CheckedAt, ErrorKind: monitoring.ErrorTLS}},
		{name: "other", result: monitoring.CheckResult{ServiceID: base.ServiceID, CheckedAt: base.CheckedAt, ErrorKind: monitoring.ErrorOther}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTestTracker(t)
			guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
			var saved []monitoring.CheckResult
			writer := checkResultWriterFunc(func(_ context.Context, result monitoring.CheckResult) error {
				saved = append(saved, result)
				return nil
			})
			results := make(chan monitoring.CheckResult, 1)
			results <- tt.result
			close(results)
			if err := processResults(context.Background(), results, writer, tracker, newTestIncidentProcessor(), successfulEventPublisher(), guard); err == nil || !strings.Contains(err.Error(), "results channel closed") {
				t.Fatalf("processResults() error = %v, want active-channel closure", err)
			}
			if len(saved) != 1 || saved[0] != tt.result {
				t.Fatalf("saved results = %+v, want one raw %+v", saved, tt.result)
			}
			if _, outstanding := guard.outstanding["example"]; outstanding {
				t.Fatal("processed failure left service outstanding")
			}
		})
	}
}

func TestMonitoringRuntimeFailsOnPersistenceErrorAndJoinsChecks(t *testing.T) {
	startedSibling := make(chan struct{})
	stoppedSibling := make(chan struct{})
	checker := checkerFunc(func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		if svc.ID == "sibling" {
			close(startedSibling)
			<-ctx.Done()
			close(stoppedSibling)
			return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: time.Now().UTC(), ErrorKind: monitoring.ErrorCanceled}
		}
		select {
		case <-startedSibling:
		case <-ctx.Done():
			return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: time.Now().UTC(), ErrorKind: monitoring.ErrorCanceled}
		}
		return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: time.Now().UTC(), StatusCode: 200}
	})
	wantErr := errors.New("database write failed")
	var saveCalls atomic.Int32
	writer := checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error {
		saveCalls.Add(1)
		return wantErr
	})
	runtime, err := NewMonitoringRuntime(
		[]service.Service{{ID: "primary", Enabled: true}, {ID: "sibling", Enabled: true}},
		checker, writer, newTestIncidentProcessor(), successfulEventPublisher(), time.Hour, 2, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.pool.Submit(context.Background(), service.Service{ID: "sibling", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(runCtx) }()
	if err := waitError(t, done); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want persistence failure", err)
	}
	waitSignal(t, stoppedSibling)
	if got := saveCalls.Load(); got != 1 {
		t.Fatalf("Save calls = %d, want 1", got)
	}
	snapshot, err := runtime.Tracker().Get("primary")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUnknown || !snapshot.LastObservedAt.IsZero() {
		t.Fatalf("tracker changed after failed Save: %+v", snapshot)
	}
	assertResultsClosed(t, runtime.pool.Results())
}

func TestProcessResultsTreatsContextSaveErrorAsFatalWhileActive(t *testing.T) {
	tracker := newTestTracker(t)
	guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
	result := monitoring.CheckResult{ServiceID: "example", CheckedAt: time.Now().UTC(), StatusCode: 200}
	results := make(chan monitoring.CheckResult, 1)
	results <- result
	writer := checkResultWriterFunc(func(ctx context.Context, _ monitoring.CheckResult) error {
		if ctx.Err() != nil {
			return errors.New("context unexpectedly canceled before Save")
		}
		return fmt.Errorf("writer: %w", context.Canceled)
	})
	err := processResults(context.Background(), results, writer, tracker, newTestIncidentProcessor(), successfulEventPublisher(), guard)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "save check result") {
		t.Fatalf("processResults() error = %v, want wrapped fatal Save cancellation", err)
	}
	snapshot, err := tracker.Get("example")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUnknown || !snapshot.LastObservedAt.IsZero() {
		t.Fatalf("tracker changed after failed Save: %+v", snapshot)
	}
}

func TestProcessResultsDoesNotHideUnrelatedSaveErrorDuringShutdown(t *testing.T) {
	tracker := newTestTracker(t)
	guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wantErr := errors.New("database unavailable")
	writer := checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error {
		cancel()
		return wantErr
	})
	results := make(chan monitoring.CheckResult, 1)
	results <- monitoring.CheckResult{ServiceID: "example", CheckedAt: time.Now().UTC(), StatusCode: 200}
	if err := processResults(ctx, results, writer, tracker, newTestIncidentProcessor(), successfulEventPublisher(), guard); !errors.Is(err, wantErr) {
		t.Fatalf("processResults() error = %v, want real Save error", err)
	}
	snapshot, err := tracker.Get("example")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUnknown || !snapshot.LastObservedAt.IsZero() {
		t.Fatalf("tracker changed after failed Save: %+v", snapshot)
	}
}

func TestProcessResultsRejectsBufferedResultAfterCancellation(t *testing.T) {
	tracker := newTestTracker(t)
	guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
	var saveCalls atomic.Int32
	writer := checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error {
		saveCalls.Add(1)
		return nil
	})
	results := make(chan monitoring.CheckResult, 1)
	results <- monitoring.CheckResult{ServiceID: "example", CheckedAt: time.Now().UTC(), StatusCode: 200}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := processResults(ctx, results, writer, tracker, newTestIncidentProcessor(), successfulEventPublisher(), guard); err != nil {
		t.Fatalf("processResults() after cancellation = %v, want nil", err)
	}
	if got := saveCalls.Load(); got != 0 {
		t.Fatalf("Save calls after cutoff = %d, want 0", got)
	}
	snapshot, err := tracker.Get("example")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUnknown || !snapshot.LastObservedAt.IsZero() {
		t.Fatalf("tracker changed after cutoff: %+v", snapshot)
	}
}

func TestMonitoringRuntimeStopsCleanlyWhenSaveMatchesCanceledContext(t *testing.T) {
	startedSave := make(chan struct{})
	writer := checkResultWriterFunc(func(ctx context.Context, _ monitoring.CheckResult) error {
		close(startedSave)
		<-ctx.Done()
		return fmt.Errorf("save interrupted: %w", ctx.Err())
	})
	runtime, err := NewMonitoringRuntime(
		[]service.Service{{ID: "example", Enabled: true}},
		checkerFunc(func(context.Context, service.Service) monitoring.CheckResult {
			return monitoring.CheckResult{ServiceID: "example", CheckedAt: time.Now().UTC(), StatusCode: 200}
		}),
		writer, newTestIncidentProcessor(), successfulEventPublisher(), time.Hour, 1, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	waitSignal(t, startedSave)
	cancel()
	if err := waitError(t, done); err != nil {
		t.Fatalf("Run() after canceled Save = %v, want nil", err)
	}
	snapshot, err := runtime.Tracker().Get("example")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUnknown || !snapshot.LastObservedAt.IsZero() {
		t.Fatalf("tracker changed after canceled Save: %+v", snapshot)
	}
	assertResultsClosed(t, runtime.pool.Results())
}

func TestMonitoringRuntimeFinishesAcceptedResultAfterCancellation(t *testing.T) {
	startedSave := make(chan struct{})
	var saveCalls atomic.Int32
	published := make(chan event.Event, 1)
	writer := checkResultWriterFunc(func(ctx context.Context, _ monitoring.CheckResult) error {
		saveCalls.Add(1)
		close(startedSave)
		<-ctx.Done()
		return nil
	})
	runtime, err := NewMonitoringRuntime(
		[]service.Service{{ID: "example", Enabled: true}},
		checkerFunc(func(context.Context, service.Service) monitoring.CheckResult {
			return monitoring.CheckResult{ServiceID: "example", CheckedAt: time.Now().UTC(), StatusCode: 200}
		}),
		writer,
		newTestIncidentProcessor(),
		eventPublisherFunc(func(value event.Event) error {
			published <- value
			return nil
		}),
		time.Hour, 1, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	waitSignal(t, startedSave)
	cancel()
	if err := waitError(t, done); err != nil {
		t.Fatalf("Run() after successful Save during shutdown = %v, want nil", err)
	}
	if got := saveCalls.Load(); got != 1 {
		t.Fatalf("Save calls = %d, want 1", got)
	}
	snapshot, err := runtime.Tracker().Get("example")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUp || snapshot.LastObservedAt.IsZero() {
		t.Fatalf("accepted result did not reach tracker: %+v", snapshot)
	}
	select {
	case value := <-published:
		changed, ok := value.(event.StatusChanged)
		if !ok || changed.ServiceID != "example" || changed.State != status.StatusUp || !changed.LastCheckedAt.Equal(snapshot.LastObservedAt) {
			t.Fatalf("published event = %#v, want accepted UP transition", value)
		}
	default:
		t.Fatal("accepted result was not published during cancellation")
	}
	if _, outstanding := runtime.submitter.outstanding["example"]; outstanding {
		t.Fatal("accepted result left service outstanding")
	}
	assertResultsClosed(t, runtime.pool.Results())
}
