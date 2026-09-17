package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
)

const testTimeout = 5 * time.Second

type checkerFunc func(context.Context, service.Service) monitoring.CheckResult

func (f checkerFunc) Check(ctx context.Context, svc service.Service) monitoring.CheckResult {
	return f(ctx, svc)
}

type submitterFunc func(context.Context, service.Service) error

func (f submitterFunc) Submit(ctx context.Context, svc service.Service) error {
	return f(ctx, svc)
}

type checkResultWriterFunc func(context.Context, monitoring.CheckResult) error

func (f checkResultWriterFunc) Save(ctx context.Context, result monitoring.CheckResult) error {
	return f(ctx, result)
}

func TestNewMonitoringRuntimeRequiresWriter(t *testing.T) {
	_, err := NewMonitoringRuntime(
		[]service.Service{{ID: "example", Enabled: true}},
		checkerFunc(func(context.Context, service.Service) monitoring.CheckResult { return monitoring.CheckResult{} }),
		nil,
		time.Hour,
		1,
		1,
	)
	if err == nil || !strings.Contains(err.Error(), "writer is required") {
		t.Fatalf("NewMonitoringRuntime() error = %v, want required writer error", err)
	}
}

func TestNewMonitoringRuntimeUsesOneServiceSnapshot(t *testing.T) {
	services := []service.Service{{ID: "original", Enabled: true}}
	checked := make(chan string, 1)
	runtime := newTestRuntime(t, services, checkerFunc(func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		checked <- svc.ID
		<-ctx.Done()
		return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: time.Now().UTC(), ErrorKind: monitoring.ErrorCanceled}
	}))
	services[0].ID = "changed"

	snapshots := runtime.Tracker().Snapshot()
	if len(snapshots) != 1 || snapshots[0].ServiceID != "original" || snapshots[0].Status != status.StatusUnknown {
		t.Fatalf("initial snapshots = %+v, want one unknown original service", snapshots)
	}
	if runtime.Tracker() != runtime.tracker {
		t.Fatal("Tracker() did not return the runtime's authoritative tracker")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	select {
	case serviceID := <-checked:
		if serviceID != "original" {
			t.Fatalf("scheduled service ID = %q, want original", serviceID)
		}
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for scheduled check")
	}
	cancel()
	if err := waitError(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestProcessResultsEvaluatesAndAppliesObservations(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		codes []int
		want  status.ServiceStatus
	}{
		{name: "healthy result moves unknown to up", codes: []int{200}, want: status.StatusUp},
		{name: "three failures move unknown to down", codes: []int{503, 503, 503}, want: status.StatusDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTestTracker(t)
			guard := &outstandingSubmitter{outstanding: map[string]struct{}{"example": {}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			saved := 0
			writer := checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error {
				saved++
				if saved == len(tt.codes) {
					cancel()
				}
				return nil
			})

			results := make(chan monitoring.CheckResult, len(tt.codes))
			for index, code := range tt.codes {
				results <- monitoring.CheckResult{
					ServiceID:  "example",
					CheckedAt:  base.Add(time.Duration(index) * time.Second),
					StatusCode: code,
				}
			}
			close(results)
			if err := processResults(ctx, results, writer, tracker, guard); err != nil {
				t.Fatalf("processResults() error = %v", err)
			}
			if saved != len(tt.codes) {
				t.Fatalf("saved results = %d, want %d", saved, len(tt.codes))
			}
			if _, outstanding := guard.outstanding["example"]; outstanding {
				t.Fatal("processed result left service outstanding")
			}

			snapshot, err := tracker.Get("example")
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Status != tt.want {
				t.Fatalf("status = %q, want %q", snapshot.Status, tt.want)
			}
		})
	}
}

func TestProcessResultsReleasesIgnoredAndStaleResults(t *testing.T) {
	tracker := newTestTracker(t)
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	if _, err := tracker.Apply(monitoring.Observation{
		ServiceID: "example", ObservedAt: base, Kind: monitoring.ObservationHealthy,
	}); err != nil {
		t.Fatal(err)
	}

	guard := &outstandingSubmitter{outstanding: make(map[string]struct{})}
	var saved []monitoring.CheckResult
	for _, result := range []monitoring.CheckResult{
		{ServiceID: "example", CheckedAt: base.Add(time.Second), ErrorKind: monitoring.ErrorCanceled},
		{ServiceID: "example", CheckedAt: base.Add(-time.Second), StatusCode: 503},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		writer := checkResultWriterFunc(func(_ context.Context, result monitoring.CheckResult) error {
			saved = append(saved, result)
			cancel()
			return nil
		})
		guard.outstanding["example"] = struct{}{}
		results := make(chan monitoring.CheckResult, 1)
		results <- result
		close(results)
		if err := processResults(ctx, results, writer, tracker, guard); err != nil {
			t.Fatal(err)
		}
		cancel()
		if _, outstanding := guard.outstanding["example"]; outstanding {
			t.Fatal("no-op result left service outstanding")
		}
	}
	snapshot, err := tracker.Get("example")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != status.StatusUp || !snapshot.LastObservedAt.Equal(base) {
		t.Fatalf("snapshot after ignored and stale results = %+v", snapshot)
	}
	if len(saved) != 2 || saved[0].ErrorKind != monitoring.ErrorCanceled || saved[1].StatusCode != 503 {
		t.Fatalf("saved no-op results = %+v, want canceled and stale raw checks", saved)
	}
}

func TestOutstandingSubmitterSkipsUntilResultProcessed(t *testing.T) {
	calls := 0
	guard := &outstandingSubmitter{
		delegate: submitterFunc(func(context.Context, service.Service) error {
			calls++
			return nil
		}),
		outstanding: make(map[string]struct{}),
	}
	svc := service.Service{ID: "example"}
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("delegate calls = %d, want 1 while service is outstanding", calls)
	}
	guard.complete(svc.ID)
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("delegate calls = %d, want 2 after result processing", calls)
	}
}

func TestOutstandingSubmitterRollsBackFailedSubmission(t *testing.T) {
	wantErr := errors.New("submit failed")
	calls := 0
	guard := &outstandingSubmitter{
		delegate: submitterFunc(func(context.Context, service.Service) error {
			calls++
			if calls == 1 {
				return wantErr
			}
			return nil
		}),
		outstanding: make(map[string]struct{}),
	}
	svc := service.Service{ID: "example"}
	if err := guard.Submit(context.Background(), svc); !errors.Is(err, wantErr) {
		t.Fatalf("first Submit() error = %v, want %v", err, wantErr)
	}
	if err := guard.Submit(context.Background(), svc); err != nil {
		t.Fatalf("second Submit() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("delegate calls = %d, want 2 after rollback", calls)
	}
}

func TestBlockedSubmissionDoesNotHoldOutstandingMutex(t *testing.T) {
	aStarted := make(chan struct{})
	releaseA := make(chan struct{})
	var releaseOnce sync.Once
	unblockA := func() { releaseOnce.Do(func() { close(releaseA) }) }
	defer unblockA()

	bSubmitted := make(chan struct{}, 2)
	guard := &outstandingSubmitter{
		delegate: submitterFunc(func(_ context.Context, svc service.Service) error {
			if svc.ID == "a" {
				close(aStarted)
				<-releaseA
				return nil
			}
			bSubmitted <- struct{}{}
			return nil
		}),
		outstanding: make(map[string]struct{}),
	}
	aDone := make(chan error, 1)
	go func() { aDone <- guard.Submit(context.Background(), service.Service{ID: "a"}) }()
	waitSignal(t, aStarted)

	bDone := make(chan error, 1)
	go func() { bDone <- guard.Submit(context.Background(), service.Service{ID: "b"}) }()
	waitSignal(t, bSubmitted)
	if err := waitError(t, bDone); err != nil {
		t.Fatal(err)
	}

	released := make(chan struct{})
	go func() {
		guard.complete("b")
		close(released)
	}()
	waitSignal(t, released)
	go func() { bDone <- guard.Submit(context.Background(), service.Service{ID: "b"}) }()
	waitSignal(t, bSubmitted)
	if err := waitError(t, bDone); err != nil {
		t.Fatal(err)
	}

	unblockA()
	if err := waitError(t, aDone); err != nil {
		t.Fatal(err)
	}
}

func TestMonitoringRuntimeCancellationJoinsActiveCheck(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var saved atomic.Int32
	runtime := newTestRuntime(t, []service.Service{{ID: "example", Enabled: true}}, checkerFunc(func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		close(started)
		<-ctx.Done()
		close(stopped)
		return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: time.Now().UTC(), ErrorKind: monitoring.ErrorCanceled}
	}))
	runtime.writer = checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error {
		saved.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	waitSignal(t, started)
	cancel()
	waitSignal(t, stopped)
	if err := waitError(t, done); err != nil {
		t.Fatalf("Run() after cancellation = %v, want nil", err)
	}
	if got := saved.Load(); got != 0 {
		t.Fatalf("shutdown-canceled check saved %d times, want 0", got)
	}
	assertResultsClosed(t, runtime.pool.Results())
}

func TestMonitoringRuntimePreservesSchedulerFailure(t *testing.T) {
	svc := service.Service{ID: "example", Enabled: true}
	var checkerCalled atomic.Bool
	runtime := newTestRuntime(t, []service.Service{svc}, checkerFunc(func(context.Context, service.Service) monitoring.CheckResult {
		checkerCalled.Store(true)
		return monitoring.CheckResult{}
	}))
	wantErr := errors.New("scheduler submission failed")
	scheduler, err := monitoring.NewScheduler(submitterFunc(func(context.Context, service.Service) error {
		return wantErr
	}), []service.Service{svc}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	runtime.scheduler = scheduler
	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	if err := waitError(t, done); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want scheduler failure", err)
	}
	if checkerCalled.Load() {
		t.Fatal("checker ran after failed scheduler submission")
	}
	assertResultsClosed(t, runtime.pool.Results())
}

func TestMonitoringRuntimeFailsOnTrackerError(t *testing.T) {
	runtime := newTestRuntime(t, []service.Service{{ID: "example", Enabled: true}}, checkerFunc(func(context.Context, service.Service) monitoring.CheckResult {
		return monitoring.CheckResult{ServiceID: "unknown", CheckedAt: time.Now().UTC(), StatusCode: 200}
	}))
	var saved atomic.Int32
	runtime.writer = checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error {
		saved.Add(1)
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	if err := waitError(t, done); !errors.Is(err, status.ErrUnknownService) {
		t.Fatalf("Run() error = %v, want ErrUnknownService", err)
	}
	if got := saved.Load(); got != 1 {
		t.Fatalf("Save calls before tracker failure = %d, want 1", got)
	}
	assertResultsClosed(t, runtime.pool.Results())
}

func TestMonitoringRuntimeFailsIfWorkerPoolAlreadyStopped(t *testing.T) {
	runtime := newTestRuntime(t, []service.Service{{ID: "example", Enabled: true}}, checkerFunc(func(context.Context, service.Service) monitoring.CheckResult {
		return monitoring.CheckResult{}
	}))
	stoppedCtx, cancel := context.WithCancel(context.Background())
	cancel()
	runtime.pool.Run(stoppedCtx)

	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	if err := waitError(t, done); err == nil {
		t.Fatal("Run() error = nil with an already-stopped worker pool")
	}
	assertResultsClosed(t, runtime.pool.Results())
}

func TestProcessResultsDetectsUnexpectedClosure(t *testing.T) {
	tracker := newTestTracker(t)
	guard := &outstandingSubmitter{outstanding: make(map[string]struct{})}
	writer := checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error { return nil })
	results := make(chan monitoring.CheckResult)
	close(results)
	if err := processResults(context.Background(), results, writer, tracker, guard); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("active processResults() error = %v, want unexpected closure", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := processResults(ctx, results, writer, tracker, guard); err != nil {
		t.Fatalf("canceled processResults() error = %v, want nil", err)
	}
}

func TestMonitoringRuntimeIsSingleUse(t *testing.T) {
	started := make(chan struct{})
	runtime := newTestRuntime(t, []service.Service{{ID: "example", Enabled: true}}, checkerFunc(func(ctx context.Context, svc service.Service) monitoring.CheckResult {
		close(started)
		<-ctx.Done()
		return monitoring.CheckResult{ServiceID: svc.ID, CheckedAt: time.Now().UTC(), ErrorKind: monitoring.ErrorCanceled}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	waitSignal(t, started)
	if err := runtime.Run(context.Background()); !errors.Is(err, errRuntimeAlreadyRun) {
		t.Fatalf("second active Run() error = %v, want single-use error", err)
	}
	cancel()
	if err := waitError(t, done); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Run(context.Background()); !errors.Is(err, errRuntimeAlreadyRun) {
		t.Fatalf("Run() after termination error = %v, want single-use error", err)
	}
}

func newTestRuntime(t *testing.T, services []service.Service, checker monitoring.Checker) *MonitoringRuntime {
	t.Helper()
	runtime, err := NewMonitoringRuntime(services, checker, checkResultWriterFunc(func(context.Context, monitoring.CheckResult) error {
		return nil
	}), time.Hour, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func newTestTracker(t *testing.T) *status.Tracker {
	t.Helper()
	tracker, err := status.NewTracker([]service.Service{{ID: "example", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	return tracker
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for signal")
	}
}

func waitError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for completion")
		return nil
	}
}

func assertResultsClosed(t *testing.T, results <-chan monitoring.CheckResult) {
	t.Helper()
	timer := time.NewTimer(testTimeout)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-results:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("results channel was not closed")
		}
	}
}
