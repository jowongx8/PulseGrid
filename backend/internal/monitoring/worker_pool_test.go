package monitoring

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/service"
)

const workerPoolTestTimeout = time.Second

func TestNewWorkerPoolValidation(t *testing.T) {
	checker := &recordingPoolChecker{}

	tests := []struct {
		name          string
		checker       Checker
		workerCount   int
		queueCapacity int
	}{
		{
			name:          "nil checker",
			checker:       nil,
			workerCount:   1,
			queueCapacity: 1,
		},
		{
			name:          "zero workers",
			checker:       checker,
			workerCount:   0,
			queueCapacity: 1,
		},
		{
			name:          "negative workers",
			checker:       checker,
			workerCount:   -1,
			queueCapacity: 1,
		},
		{
			name:          "zero queue capacity",
			checker:       checker,
			workerCount:   1,
			queueCapacity: 0,
		},
		{
			name:          "negative queue capacity",
			checker:       checker,
			workerCount:   1,
			queueCapacity: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := NewWorkerPool(tt.checker, tt.workerCount, tt.queueCapacity)
			if err == nil {
				t.Fatalf("NewWorkerPool() error = nil, want validation error")
			}

			if pool != nil {
				t.Fatal("NewWorkerPool() returned pool for invalid input")
			}
		})
	}
}

func TestWorkerPoolChecksAcceptedJobsExactlyOnce(t *testing.T) {
	checker := &recordingPoolChecker{}
	services := []service.Service{
		poolService("github"),
		poolService("slack"),
		poolService("amazon"),
		poolService("github"),
	}

	pool := newTestWorkerPool(t, checker, 2, len(services))
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	for _, svc := range services {
		if err := pool.Submit(context.Background(), svc); err != nil {
			t.Fatalf("Submit(%q) error = %v", svc.ID, err)
		}
	}

	resultsByService := make(map[string]int)
	for range services {
		result := receiveResult(t, pool.Results())
		resultsByService[result.ServiceID]++
	}

	cancelRun()
	waitClosed(t, runDone, "worker pool run")

	if got := checker.callCount("github"); got != 2 {
		t.Fatalf("github checks = %d, want 2", got)
	}

	if got := checker.callCount("slack"); got != 1 {
		t.Fatalf("slack checks = %d, want 1", got)
	}

	if got := checker.callCount("amazon"); got != 1 {
		t.Fatalf("amazon checks = %d, want 1", got)
	}

	if got := resultsByService["github"]; got != 2 {
		t.Fatalf("github results = %d, want 2", got)
	}

	if got := resultsByService["slack"]; got != 1 {
		t.Fatalf("slack results = %d, want 1", got)
	}

	if got := resultsByService["amazon"]; got != 1 {
		t.Fatalf("amazon results = %d, want 1", got)
	}
}

func TestWorkerPoolBoundsConcurrentChecks(t *testing.T) {
	const workerCount = 3
	const jobCount = 20

	checker := newConcurrencyTrackingChecker(jobCount)
	pool := newTestWorkerPool(t, checker, workerCount, jobCount)

	for i := range jobCount {
		if err := pool.Submit(context.Background(), poolService(string(rune('a'+i)))); err != nil {
			t.Fatalf("Submit(%d) error = %v", i, err)
		}
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	for range workerCount {
		waitStarted(t, checker.started)
	}

	if max := checker.maxActive.Load(); max != workerCount {
		t.Fatalf("maximum active checks before release = %d, want %d", max, workerCount)
	}

	close(checker.release)

	for range jobCount {
		receiveResult(t, pool.Results())
	}

	cancelRun()
	waitClosed(t, runDone, "worker pool run")

	if max := checker.maxActive.Load(); max > workerCount {
		t.Fatalf("maximum active checks = %d, want at most %d", max, workerCount)
	}
}

func TestWorkerPoolSubmitAppliesBackpressure(t *testing.T) {
	checker := newBlockingPoolChecker()
	pool := newTestWorkerPool(t, checker, 1, 1)
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	if err := pool.Submit(context.Background(), poolService("active")); err != nil {
		t.Fatalf("Submit(active) error = %v", err)
	}
	if got := waitString(t, checker.started, "checker start"); got != "active" {
		t.Fatalf("first started check = %q, want active", got)
	}

	if err := pool.Submit(context.Background(), poolService("queued")); err != nil {
		t.Fatalf("Submit(queued) error = %v", err)
	}

	submitDone := make(chan error, 1)
	go func() {
		submitDone <- pool.Submit(context.Background(), poolService("blocked"))
	}()

	select {
	case err := <-submitDone:
		t.Fatalf("blocked Submit returned before queue capacity was available: %v", err)
	default:
	}

	close(checker.release)

	select {
	case err := <-submitDone:
		if err != nil {
			t.Fatalf("blocked Submit error = %v", err)
		}
	case <-time.After(workerPoolTestTimeout):
		t.Fatal("blocked Submit did not return after queue capacity became available")
	}

	cancelRun()
	waitClosed(t, runDone, "worker pool run")
}

func TestWorkerPoolSubmitHonorsCancellationAndDeadlines(t *testing.T) {
	pool := newTestWorkerPool(t, &recordingPoolChecker{}, 1, 1)
	if err := pool.Submit(context.Background(), poolService("filler")); err != nil {
		t.Fatalf("Submit(filler) error = %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := pool.Submit(canceledCtx, poolService("canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Submit() error = %v, want context.Canceled", err)
	}

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Nanosecond))
	defer deadlineCancel()

	if err := pool.Submit(deadlineCtx, poolService("deadline")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Submit() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestWorkerPoolAlreadyCanceledSubmitDoesNotEnqueueWhenCapacityIsAvailable(t *testing.T) {
	pool := newTestWorkerPool(t, &recordingPoolChecker{}, 1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := pool.Submit(ctx, poolService("canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Submit() error = %v, want context.Canceled", err)
	}

	if queued := len(pool.jobs); queued != 0 {
		t.Fatalf("queued jobs = %d, want 0", queued)
	}
}

func TestWorkerPoolSubmitContextDoesNotControlCheckLifetime(t *testing.T) {
	submitCtx, cancelSubmit := context.WithCancel(context.Background())
	checker := &submitContextSeparationChecker{
		submitDone: submitCtx.Done(),
		ctxErr:     make(chan error, 1),
	}
	pool := newTestWorkerPool(t, checker, 1, 1)
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	if err := pool.Submit(submitCtx, poolService("github")); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	cancelSubmit()

	if err := waitError(t, checker.ctxErr, "checker context observation"); err != nil {
		t.Fatalf("checker received canceled context from Submit: %v", err)
	}

	result := receiveResult(t, pool.Results())
	if result.ServiceID != "github" {
		t.Fatalf("result service ID = %q, want github", result.ServiceID)
	}

	cancelRun()
	waitClosed(t, runDone, "worker pool run")
}

func TestWorkerPoolRunCancellationCancelsActiveChecksAndClosesResults(t *testing.T) {
	checker := &contextWaitingChecker{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
	pool := newTestWorkerPool(t, checker, 1, 1)
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	if err := pool.Submit(context.Background(), poolService("github")); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	waitClosed(t, checker.started, "checker start")

	cancelRun()
	waitClosed(t, checker.done, "checker cancellation")
	waitClosed(t, runDone, "worker pool run")
	drainClosedResults(t, pool.Results())
}

func TestWorkerPoolRunIsSingleUse(t *testing.T) {
	pool := newTestWorkerPool(t, &recordingPoolChecker{}, 1, 1)

	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstCancel()
	pool.Run(firstCtx)
	drainClosedResults(t, pool.Results())

	secondReturned := make(chan struct{})
	go func() {
		defer close(secondReturned)
		pool.Run(context.Background())
	}()

	waitClosed(t, secondReturned, "second Run call")
}

func TestWorkerPoolSecondRunWhileActiveDoesNotStartMoreWorkers(t *testing.T) {
	checker := newBlockingPoolChecker()
	pool := newTestWorkerPool(t, checker, 1, 2)
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	if err := pool.Submit(context.Background(), poolService("active")); err != nil {
		t.Fatalf("Submit(active) error = %v", err)
	}
	if got := waitString(t, checker.started, "checker start"); got != "active" {
		t.Fatalf("started check = %q, want active", got)
	}

	secondReturned := make(chan struct{})
	go func() {
		defer close(secondReturned)
		pool.Run(context.Background())
	}()
	waitClosed(t, secondReturned, "second Run call")

	if err := pool.Submit(context.Background(), poolService("queued")); err != nil {
		t.Fatalf("Submit(queued) error = %v", err)
	}

	select {
	case got := <-checker.started:
		t.Fatalf("unexpected extra worker started check %q", got)
	default:
	}

	cancelRun()
	waitClosed(t, runDone, "worker pool run")
}

func TestWorkerPoolSubmitAfterRunTerminatesFails(t *testing.T) {
	pool := newTestWorkerPool(t, &recordingPoolChecker{}, 1, 1)

	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()
	pool.Run(runCtx)

	if err := pool.Submit(context.Background(), poolService("late")); !errors.Is(err, errWorkerPoolStopped) {
		t.Fatalf("Submit() error = %v, want %v", err, errWorkerPoolStopped)
	}

	if queued := len(pool.jobs); queued != 0 {
		t.Fatalf("queued jobs = %d, want 0", queued)
	}
}

func TestWorkerPoolDoesNotDrainQueuedWorkAfterShutdown(t *testing.T) {
	checker := newBlockingPoolChecker()
	pool := newTestWorkerPool(t, checker, 1, 3)
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	if err := pool.Submit(context.Background(), poolService("active")); err != nil {
		t.Fatalf("Submit(active) error = %v", err)
	}
	if got := waitString(t, checker.started, "checker start"); got != "active" {
		t.Fatalf("started check = %q, want active", got)
	}

	if err := pool.Submit(context.Background(), poolService("queued-1")); err != nil {
		t.Fatalf("Submit(queued-1) error = %v", err)
	}
	if err := pool.Submit(context.Background(), poolService("queued-2")); err != nil {
		t.Fatalf("Submit(queued-2) error = %v", err)
	}

	cancelRun()
	waitClosed(t, runDone, "worker pool run")

	started := checker.startedServices()
	if len(started) != 1 || started[0] != "active" {
		t.Fatalf("started checks = %v, want only [active]", started)
	}
}

func TestWorkerPoolResultsArriveInCompletionOrder(t *testing.T) {
	checker := newReleasablePoolChecker("slow", "fast")
	pool := newTestWorkerPool(t, checker, 2, 2)
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	if err := pool.Submit(context.Background(), poolService("slow")); err != nil {
		t.Fatalf("Submit(slow) error = %v", err)
	}
	if err := pool.Submit(context.Background(), poolService("fast")); err != nil {
		t.Fatalf("Submit(fast) error = %v", err)
	}

	started := map[string]bool{
		waitString(t, checker.started, "checker start"): true,
		waitString(t, checker.started, "checker start"): true,
	}
	if !started["slow"] || !started["fast"] {
		t.Fatalf("started checks = %v, want slow and fast", started)
	}

	checker.release("fast")
	first := receiveResult(t, pool.Results())
	if first.ServiceID != "fast" {
		t.Fatalf("first result = %q, want fast", first.ServiceID)
	}

	checker.release("slow")
	second := receiveResult(t, pool.Results())
	if second.ServiceID != "slow" {
		t.Fatalf("second result = %q, want slow", second.ServiceID)
	}

	cancelRun()
	waitClosed(t, runDone, "worker pool run")
}

func TestWorkerPoolCancellationUnblocksSlowResultConsumer(t *testing.T) {
	checker := &notifyingImmediateChecker{
		calls: make(chan string, 2),
	}
	pool := newTestWorkerPool(t, checker, 1, 2)
	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := runPool(t, pool, runCtx)

	if err := pool.Submit(context.Background(), poolService("first")); err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	if got := waitString(t, checker.calls, "first check"); got != "first" {
		t.Fatalf("first check = %q, want first", got)
	}

	if err := pool.Submit(context.Background(), poolService("second")); err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	if got := waitString(t, checker.calls, "second check"); got != "second" {
		t.Fatalf("second check = %q, want second", got)
	}

	cancelRun()
	waitClosed(t, runDone, "worker pool run")
}

func newTestWorkerPool(t *testing.T, checker Checker, workerCount int, queueCapacity int) *WorkerPool {
	t.Helper()

	pool, err := NewWorkerPool(checker, workerCount, queueCapacity)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}

	return pool
}

func runPool(t *testing.T, pool *WorkerPool, ctx context.Context) <-chan struct{} {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.Run(ctx)
	}()

	return done
}

func receiveResult(t *testing.T, results <-chan CheckResult) CheckResult {
	t.Helper()

	select {
	case result, ok := <-results:
		if !ok {
			t.Fatal("results channel closed before result was received")
		}

		return result
	case <-time.After(workerPoolTestTimeout):
		t.Fatal("timed out waiting for result")
	}

	return CheckResult{}
}

func waitStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()

	select {
	case <-started:
	case <-time.After(workerPoolTestTimeout):
		t.Fatal("timed out waiting for checker to start")
	}
}

func waitString(t *testing.T, ch <-chan string, label string) string {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(workerPoolTestTimeout):
		t.Fatalf("timed out waiting for %s", label)
	}

	return ""
}

func waitError(t *testing.T, ch <-chan error, label string) error {
	t.Helper()

	select {
	case err := <-ch:
		return err
	case <-time.After(workerPoolTestTimeout):
		t.Fatalf("timed out waiting for %s", label)
	}

	return nil
}

func waitClosed(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(workerPoolTestTimeout):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func drainClosedResults(t *testing.T, results <-chan CheckResult) {
	t.Helper()

	for {
		select {
		case _, ok := <-results:
			if !ok {
				return
			}
		case <-time.After(workerPoolTestTimeout):
			t.Fatal("timed out waiting for results channel to close")
		}
	}
}

func poolService(id string) service.Service {
	return service.Service{
		ID:       id,
		CheckURL: "https://example.test/" + id,
		Enabled:  false,
	}
}

type recordingPoolChecker struct {
	mu    sync.Mutex
	calls map[string]int
}

func (c *recordingPoolChecker) Check(_ context.Context, svc service.Service) CheckResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.calls == nil {
		c.calls = make(map[string]int)
	}
	c.calls[svc.ID]++

	return CheckResult{
		ServiceID:  svc.ID,
		CheckedAt:  time.Now().UTC(),
		StatusCode: 200,
	}
}

func (c *recordingPoolChecker) callCount(serviceID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.calls[serviceID]
}

type concurrencyTrackingChecker struct {
	started   chan struct{}
	release   chan struct{}
	active    atomic.Int32
	maxActive atomic.Int32
}

func newConcurrencyTrackingChecker(jobCount int) *concurrencyTrackingChecker {
	return &concurrencyTrackingChecker{
		started: make(chan struct{}, jobCount),
		release: make(chan struct{}),
	}
}

func (c *concurrencyTrackingChecker) Check(ctx context.Context, svc service.Service) CheckResult {
	active := c.active.Add(1)
	c.recordMax(active)
	c.started <- struct{}{}

	select {
	case <-c.release:
	case <-ctx.Done():
	}

	c.active.Add(-1)
	return CheckResult{
		ServiceID:  svc.ID,
		CheckedAt:  time.Now().UTC(),
		StatusCode: 200,
	}
}

func (c *concurrencyTrackingChecker) recordMax(active int32) {
	for {
		current := c.maxActive.Load()
		if active <= current {
			return
		}

		if c.maxActive.CompareAndSwap(current, active) {
			return
		}
	}
}

type blockingPoolChecker struct {
	started chan string
	release chan struct{}

	mu       sync.Mutex
	services []string
}

func newBlockingPoolChecker() *blockingPoolChecker {
	return &blockingPoolChecker{
		started: make(chan string, 16),
		release: make(chan struct{}),
	}
}

func (c *blockingPoolChecker) Check(ctx context.Context, svc service.Service) CheckResult {
	c.mu.Lock()
	c.services = append(c.services, svc.ID)
	c.mu.Unlock()

	c.started <- svc.ID
	select {
	case <-c.release:
	case <-ctx.Done():
	}

	return CheckResult{
		ServiceID:  svc.ID,
		CheckedAt:  time.Now().UTC(),
		StatusCode: 200,
	}
}

func (c *blockingPoolChecker) startedServices() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	services := make([]string, len(c.services))
	copy(services, c.services)

	return services
}

type submitContextSeparationChecker struct {
	submitDone <-chan struct{}
	ctxErr     chan error
}

func (c *submitContextSeparationChecker) Check(ctx context.Context, svc service.Service) CheckResult {
	<-c.submitDone
	c.ctxErr <- ctx.Err()

	return CheckResult{
		ServiceID:  svc.ID,
		CheckedAt:  time.Now().UTC(),
		StatusCode: 200,
	}
}

type contextWaitingChecker struct {
	started chan struct{}
	done    chan struct{}
}

func (c *contextWaitingChecker) Check(ctx context.Context, svc service.Service) CheckResult {
	close(c.started)
	<-ctx.Done()
	close(c.done)

	return CheckResult{
		ServiceID: svc.ID,
		ErrorKind: ErrorCanceled,
	}
}

type releasablePoolChecker struct {
	started  chan string
	releases map[string]chan struct{}
}

func newReleasablePoolChecker(serviceIDs ...string) *releasablePoolChecker {
	releases := make(map[string]chan struct{}, len(serviceIDs))
	for _, serviceID := range serviceIDs {
		releases[serviceID] = make(chan struct{})
	}

	return &releasablePoolChecker{
		started:  make(chan string, len(serviceIDs)),
		releases: releases,
	}
}

func (c *releasablePoolChecker) Check(ctx context.Context, svc service.Service) CheckResult {
	c.started <- svc.ID

	select {
	case <-c.releases[svc.ID]:
	case <-ctx.Done():
	}

	return CheckResult{
		ServiceID:  svc.ID,
		CheckedAt:  time.Now().UTC(),
		StatusCode: 200,
	}
}

func (c *releasablePoolChecker) release(serviceID string) {
	close(c.releases[serviceID])
}

type notifyingImmediateChecker struct {
	calls chan string
}

func (c *notifyingImmediateChecker) Check(_ context.Context, svc service.Service) CheckResult {
	c.calls <- svc.ID

	return CheckResult{
		ServiceID:  svc.ID,
		CheckedAt:  time.Now().UTC(),
		StatusCode: 200,
	}
}
