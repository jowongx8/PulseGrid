package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
)

func TestProcessResultsUsesTrackerTransitionsForIncidents(t *testing.T) {
	tracker := newTestTracker(t)
	lifecycle := incident.NewLifecycle()
	base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var events []string
	var opened, resolved incident.Incident
	writer := checkResultWriterFunc(func(_ context.Context, result monitoring.CheckResult) error {
		events = append(events, "save")
		if result.CheckedAt.Equal(base.Add(3*time.Second)) || result.CheckedAt.Equal(base.Add(5*time.Second)) {
			snapshot, err := tracker.Get("example")
			if err != nil {
				return err
			}
			want := status.StatusUp
			if result.CheckedAt.Equal(base.Add(5 * time.Second)) {
				want = status.StatusDown
			}
			if snapshot.Status != want {
				return fmt.Errorf("status at raw Save = %q, want %q", snapshot.Status, want)
			}
		}
		if result.CheckedAt.Equal(base.Add(5 * time.Second)) {
			cancel()
		}
		return nil
	})
	processor := NewIncidentProcessor(lifecycle, incidentWriterFuncs{
		open: func(gotCtx context.Context, value incident.Incident) error {
			if gotCtx != ctx {
				return errors.New("Open received another context")
			}
			snapshot, err := tracker.Get("example")
			if err != nil {
				return err
			}
			if snapshot.Status != status.StatusDown {
				return fmt.Errorf("status at incident Open = %q, want DOWN", snapshot.Status)
			}
			events = append(events, "open")
			opened = value
			return nil
		},
		resolve: func(gotCtx context.Context, value incident.Incident) error {
			if gotCtx != ctx {
				return errors.New("Resolve received another context")
			}
			snapshot, err := tracker.Get("example")
			if err != nil {
				return err
			}
			if snapshot.Status != status.StatusUp {
				return fmt.Errorf("status at incident Resolve = %q, want UP", snapshot.Status)
			}
			events = append(events, "resolve")
			resolved = value
			return nil
		},
	})
	results := make(chan monitoring.CheckResult, 6)
	for index, code := range []int{200, 503, 503, 503, 200, 200} {
		results <- monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(time.Duration(index) * time.Second), StatusCode: code}
	}
	guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
	if err := processResults(ctx, results, writer, tracker, processor, guard); err != nil {
		t.Fatal(err)
	}
	if want := []string{"save", "save", "save", "save", "open", "save", "save", "resolve"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("processing order = %v, want %v", events, want)
	}
	if opened.ServiceID != "example" || !opened.StartedAt.Equal(base.Add(3*time.Second)) || opened.ResolvedAt != nil {
		t.Fatalf("opened incident = %+v, want outage starting at threshold result", opened)
	}
	if resolved.ServiceID != "example" || !resolved.StartedAt.Equal(opened.StartedAt) ||
		resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(base.Add(5*time.Second)) {
		t.Fatalf("resolved incident = %+v, want original outage resolved at recovery threshold", resolved)
	}
	if _, active := lifecycle.Active("example"); active {
		t.Fatal("resolved incident remains active")
	}
	if _, outstanding := guard.outstanding["example"]; outstanding {
		t.Fatal("successful accepted result left service outstanding")
	}
}

func TestProcessResultsKeepsServiceOutstandingDuringIncidentWrite(t *testing.T) {
	tracker := newTestTracker(t)
	base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	primeFailures(t, tracker, base, 2)
	delegateCalls := 0
	guard := &outstandingSubmitter{
		delegate:    submitterFunc(func(context.Context, service.Service) error { delegateCalls++; return nil }),
		outstanding: make(map[string]struct{}),
	}
	svc := service.Service{ID: "example", Enabled: true}
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	processor := NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{
		open: func(context.Context, incident.Incident) error {
			close(started)
			<-release
			return nil
		},
	})
	results := make(chan monitoring.CheckResult, 1)
	results <- monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: base.Add(2 * time.Second), StatusCode: 503}
	close(results)
	done := make(chan error, 1)
	finished := make(chan struct{})
	defer func() {
		unblock()
		waitSignal(t, finished)
	}()
	go func() {
		defer close(finished)
		done <- processResults(context.Background(), results, checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error { return nil }), tracker, processor, guard)
	}()
	waitSignal(t, started)
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if delegateCalls != 1 {
		t.Fatalf("delegate calls during incident write = %d, want 1", delegateCalls)
	}
	unblock()
	if err := waitError(t, done); err == nil || !strings.Contains(err.Error(), "results channel closed") {
		t.Fatalf("processResults() error = %v, want active-channel closure after processing", err)
	}
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if delegateCalls != 2 {
		t.Fatalf("delegate calls after incident write = %d, want 2", delegateCalls)
	}
}

func TestMonitoringRuntimeFailsOnIncidentOpenAndJoinsWorkers(t *testing.T) {
	base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	startedSibling := make(chan struct{})
	stoppedSibling := make(chan struct{})
	checker := checkerFunc(func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		if svc.ID == "sibling" {
			close(startedSibling)
			<-ctx.Done()
			close(stoppedSibling)
			return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: base, ErrorKind: monitoring.ErrorCanceled}
		}
		<-startedSibling
		return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: base.Add(2 * time.Second), StatusCode: 503}
	})
	wantErr := errors.New("incident insert failed")
	lifecycle := incident.NewLifecycle()
	processor := NewIncidentProcessor(lifecycle, incidentWriterFuncs{
		open: func(context.Context, incident.Incident) error { return wantErr },
	})
	var saves atomic.Int32
	runtime, err := NewMonitoringRuntime(
		[]service.Service{{ID: "primary", Enabled: true}, {ID: "sibling", Enabled: true}},
		checker,
		checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error { saves.Add(1); return nil }),
		processor, time.Hour, 2, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 2 {
		if _, err := runtime.Tracker().Apply(monitoring.Observation{ServiceID: "primary", ObservedAt: base.Add(time.Duration(index) * time.Second), Kind: monitoring.ObservationFailure}); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.pool.Submit(context.Background(), service.Service{ID: "sibling", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	if err := waitError(t, done); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want incident persistence failure", err)
	}
	waitSignal(t, stoppedSibling)
	if got := saves.Load(); got != 1 {
		t.Fatalf("raw Save calls = %d, want 1 committed observation", got)
	}
	snapshot, err := runtime.Tracker().Get("primary")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusDown {
		t.Fatalf("status after failed incident Open = %q, want DOWN", snapshot.Status)
	}
	if _, active := lifecycle.Active("primary"); !active {
		t.Fatal("failed incident Open was rolled back in memory")
	}
	assertResultsClosed(t, runtime.pool.Results())
}

func TestProcessResultsStopsOnIncidentResolveFailure(t *testing.T) {
	base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 3)
	lifecycle := incident.NewLifecycle()
	if _, err := lifecycle.Apply(testIncidentUpdate(status.StatusUnknown, status.StatusDown, base.Add(2*time.Second))); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("incident update failed")
	processor := NewIncidentProcessor(lifecycle, incidentWriterFuncs{
		resolve: func(context.Context, incident.Incident) error { return wantErr },
	})
	guard := &outstandingSubmitter{outstanding: make(map[string]struct{})}
	var saved []monitoring.CheckResult
	writer := checkResultWriterFunc(func(_ context.Context, result monitoring.CheckResult) error {
		saved = append(saved, result)
		guard.outstanding[result.ServiceID] = struct{}{}
		return nil
	})
	results := make(chan monitoring.CheckResult, 3)
	for index := range 3 {
		results <- monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(time.Duration(index+3) * time.Second), StatusCode: 200}
	}
	err := processResults(context.Background(), results, writer, tracker, processor, guard)
	if !errors.Is(err, wantErr) {
		t.Fatalf("processResults() error = %v, want Resolve failure", err)
	}
	if len(saved) != 2 {
		t.Fatalf("raw Save calls = %d, want 2 before fatal Resolve and none after", len(saved))
	}
	snapshot, err := tracker.Get("example")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUp {
		t.Fatalf("status after failed Resolve = %q, want UP", snapshot.Status)
	}
	if _, active := lifecycle.Active("example"); active {
		t.Fatal("failed Resolve was rolled back in memory")
	}
	if _, outstanding := guard.outstanding["example"]; !outstanding {
		t.Fatal("fatal incident failure released the outstanding marker before shutdown")
	}
}

func TestProcessResultsStopsOnIncidentLifecycleError(t *testing.T) {
	base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 3)
	writes := 0
	processor := NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{
		open:    func(context.Context, incident.Incident) error { writes++; return nil },
		resolve: func(context.Context, incident.Incident) error { writes++; return nil },
	})
	saves := 0
	writer := checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error { saves++; return nil })
	results := make(chan monitoring.CheckResult, 3)
	for index := range 3 {
		results <- monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(time.Duration(index+3) * time.Second), StatusCode: 200}
	}
	guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
	err := processResults(context.Background(), results, writer, tracker, processor, guard)
	if !errors.Is(err, incident.ErrNoActiveIncident) {
		t.Fatalf("processResults() error = %v, want incident lifecycle failure", err)
	}
	if saves != 2 || writes != 0 {
		t.Fatalf("raw saves = %d, incident writes = %d; want 2 raw saves and no incident write", saves, writes)
	}
	snapshot, err := tracker.Get("example")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUp {
		t.Fatalf("status before fatal lifecycle error = %q, want UP", snapshot.Status)
	}
}

func TestProcessResultsClassifiesIncidentWriteErrorsDuringShutdown(t *testing.T) {
	wantErr := errors.New("unrelated database failure")
	tests := []struct {
		name       string
		cancel     bool
		writeError error
		wantError  error
	}{
		{name: "matching cancellation", cancel: true, writeError: fmt.Errorf("write: %w", context.Canceled)},
		{name: "unrelated error during cancellation", cancel: true, writeError: wantErr, wantError: wantErr},
		{name: "context error while active", writeError: fmt.Errorf("write: %w", context.Canceled), wantError: context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
			tracker := newTestTracker(t)
			primeFailures(t, tracker, base, 2)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lifecycle := incident.NewLifecycle()
			processor := NewIncidentProcessor(lifecycle, incidentWriterFuncs{
				open: func(gotCtx context.Context, _ incident.Incident) error {
					if gotCtx != ctx {
						return errors.New("wrong context")
					}
					if tc.cancel {
						cancel()
					}
					return tc.writeError
				},
			})
			guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
			results := make(chan monitoring.CheckResult, 1)
			results <- monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(2 * time.Second), StatusCode: 503}
			err := processResults(ctx, results, checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error { return nil }), tracker, processor, guard)
			if tc.wantError == nil && err != nil || tc.wantError != nil && !errors.Is(err, tc.wantError) {
				t.Fatalf("processResults() error = %v, want %v", err, tc.wantError)
			}
			if _, active := lifecycle.Active("example"); !active {
				t.Fatal("incident lifecycle did not retain its opened incident")
			}
			if _, outstanding := guard.outstanding["example"]; !outstanding {
				t.Fatal("failed incident write released the outstanding marker")
			}
		})
	}
}

func TestMonitoringRuntimeClassifiesIncidentWriteDuringShutdown(t *testing.T) {
	wantErr := errors.New("incident database unavailable")
	for _, tc := range []struct {
		name       string
		writeError error
		wantError  error
	}{
		{name: "matching cancellation"},
		{name: "unrelated persistence error", writeError: wantErr, wantError: wantErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
			startedWrite := make(chan struct{})
			processor := NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{
				open: func(ctx context.Context, _ incident.Incident) error {
					close(startedWrite)
					<-ctx.Done()
					if tc.writeError != nil {
						return tc.writeError
					}
					return fmt.Errorf("write interrupted: %w", ctx.Err())
				},
			})
			runtime, err := NewMonitoringRuntime(
				[]service.Service{{ID: "example", Enabled: true}},
				checkerFunc(func(context.Context, service.Service) monitoring.CheckResult {
					return monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(2 * time.Second), StatusCode: 503}
				}),
				checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error { return nil }),
				processor, time.Hour, 1, 1,
			)
			if err != nil {
				t.Fatal(err)
			}
			primeFailures(t, runtime.Tracker(), base, 2)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runtime.Run(ctx) }()
			waitSignal(t, startedWrite)
			cancel()
			if err := waitError(t, done); tc.wantError == nil && err != nil || tc.wantError != nil && !errors.Is(err, tc.wantError) {
				t.Fatalf("Run() error = %v, want %v", err, tc.wantError)
			}
			assertResultsClosed(t, runtime.pool.Results())
		})
	}
}

func TestProcessResultsFinishesAcceptedIncidentAfterSaveCancelsContext(t *testing.T) {
	base := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	tracker := newTestTracker(t)
	primeFailures(t, tracker, base, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opened incident.Incident
	processor := NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{
		open: func(gotCtx context.Context, value incident.Incident) error {
			if gotCtx != ctx || gotCtx.Err() != context.Canceled {
				return errors.New("incident writer did not receive the canceled runtime context")
			}
			opened = value
			return nil
		},
	})
	guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
	results := make(chan monitoring.CheckResult, 1)
	results <- monitoring.CheckResult{ServiceID: "example", CheckedAt: base.Add(2 * time.Second), StatusCode: 503}
	writer := checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error { cancel(); return nil })
	if err := processResults(ctx, results, writer, tracker, processor, guard); err != nil {
		t.Fatal(err)
	}
	if opened.ServiceID != "example" || !opened.StartedAt.Equal(base.Add(2*time.Second)) {
		t.Fatalf("incident after canceled Save = %+v, want accepted transition", opened)
	}
	if _, outstanding := guard.outstanding["example"]; outstanding {
		t.Fatal("successful incident write left service outstanding during shutdown")
	}
}

func primeFailures(t *testing.T, tracker *status.Tracker, base time.Time, count int) {
	t.Helper()
	for index := range count {
		if _, err := tracker.Apply(monitoring.Observation{
			ServiceID: "example", ObservedAt: base.Add(time.Duration(index) * time.Second), Kind: monitoring.ObservationFailure,
		}); err != nil {
			t.Fatal(err)
		}
	}
}
