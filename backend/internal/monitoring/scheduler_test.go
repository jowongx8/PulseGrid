package monitoring

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/service"
)

func TestNewSchedulerValidation(t *testing.T) {
	submitter := &recordingSchedulerSubmitter{}

	tests := []struct {
		name      string
		submitter Submitter
		services  []service.Service
		interval  time.Duration
	}{
		{
			name:      "nil submitter",
			submitter: nil,
			services:  []service.Service{schedulerService("github", true)},
			interval:  time.Second,
		},
		{
			name:      "zero interval",
			submitter: submitter,
			services:  []service.Service{schedulerService("github", true)},
			interval:  0,
		},
		{
			name:      "negative interval",
			submitter: submitter,
			services:  []service.Service{schedulerService("github", true)},
			interval:  -time.Second,
		},
		{
			name:      "zero enabled services",
			submitter: submitter,
			services:  []service.Service{schedulerService("github", false)},
			interval:  time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler, err := NewScheduler(tt.submitter, tt.services, tt.interval)
			if err == nil {
				t.Fatal("NewScheduler() error = nil, want validation error")
			}

			if scheduler != nil {
				t.Fatal("NewScheduler() returned scheduler for invalid input")
			}
		})
	}
}

func TestSchedulerUsesEnabledServiceSnapshotInInputOrder(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("disabled-a", false),
		schedulerService("github", true),
		schedulerService("disabled-b", false),
		schedulerService("cloudflare", true),
		schedulerService("openai", true),
	}

	scheduler := newTestScheduler(t, submitter, services, 15*time.Second, clock)
	services[1] = schedulerService("mutated", true)
	services[3].Enabled = false
	services[4].ID = "changed"

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 3)
	assertCallIDs(t, calls, []string{"github", "cloudflare", "openai"})
}

func TestSchedulerStaggersFirstCycleWithoutStartupBurst(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := schedulerServices(10)
	scheduler := newTestScheduler(t, submitter, services, 15*time.Second, clock)

	calls := runSchedulerForSubmissions(t, scheduler, submitter, len(services))
	if calls[0].at != start {
		t.Fatalf("first target = %s, want %s", calls[0].at, start)
	}

	for index, call := range calls {
		want := start.Add(15 * time.Second * time.Duration(index) / time.Duration(len(services)))
		if call.at != want {
			t.Fatalf("call %d target = %s, want %s", index, call.at, want)
		}

		if index > 0 && !call.at.After(start) {
			t.Fatalf("call %d target = %s, want after startup", index, call.at)
		}
	}
}

func TestSchedulerUsesDirectOffsetCalculationWithoutAccumulatedRounding(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := schedulerServices(6)
	scheduler := newTestScheduler(t, submitter, services, 10*time.Nanosecond, clock)

	calls := runSchedulerForSubmissions(t, scheduler, submitter, len(services))
	for index, call := range calls {
		want := start.Add(10 * time.Nanosecond * time.Duration(index) / time.Duration(len(services)))
		if call.at != want {
			t.Fatalf("call %d target = %s, want %s", index, call.at, want)
		}
	}
}

func TestSchedulerSchedulesOneEnabledServiceOncePerFullInterval(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	scheduler := newTestScheduler(t, submitter, []service.Service{schedulerService("github", true)}, 15*time.Second, clock)

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 3)
	for index, call := range calls {
		wantTarget := start.Add(time.Duration(index) * 15 * time.Second)
		wantDeadline := start.Add(time.Duration(index+1) * 15 * time.Second)
		if call.at != wantTarget {
			t.Fatalf("call %d target = %s, want %s", index, call.at, wantTarget)
		}

		if call.deadline != wantDeadline {
			t.Fatalf("call %d deadline = %s, want %s", index, call.deadline, wantDeadline)
		}
	}
}

func TestSchedulerKeepsSecondCycleAnchoredToFullInterval(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("github", true),
		schedulerService("cloudflare", true),
	}
	scheduler := newTestScheduler(t, submitter, services, 10*time.Second, clock)

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 4)
	wantTimes := []time.Time{
		start,
		start.Add(5 * time.Second),
		start.Add(10 * time.Second),
		start.Add(15 * time.Second),
	}
	assertCallTimes(t, calls, wantTimes)
	assertCallIDs(t, calls, []string{"github", "cloudflare", "github", "cloudflare"})
}

func TestSchedulerLongRunDoesNotDriftFromSubmissionDelays(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("github", true),
		schedulerService("cloudflare", true),
	}
	scheduler := newTestScheduler(t, submitter, services, 10*time.Second, clock)

	submitter.onSubmit = func(ctx context.Context, svc service.Service, callNumber int) error {
		clock.AdvanceBy(400 * time.Millisecond)
		return nil
	}

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 40)
	for slot, call := range calls {
		want := start.Add(time.Duration(slot/2) * 10 * time.Second)
		if slot%2 == 1 {
			want = want.Add(5 * time.Second)
		}

		if call.at != want {
			t.Fatalf("slot %d target = %s, want anchored target %s", slot, call.at, want)
		}
	}
}

func TestSchedulerSubmissionCanWaitUntilBeforeNextSlot(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("github", true),
		schedulerService("cloudflare", true),
	}
	scheduler := newTestScheduler(t, submitter, services, 2*time.Second, clock)

	submitter.onSubmit = func(ctx context.Context, svc service.Service, callNumber int) error {
		if callNumber == 1 {
			deadline, ok := ctx.Deadline()
			if !ok {
				return errors.New("admission context has no deadline")
			}
			clock.AdvanceTo(deadline.Add(-100 * time.Millisecond))
		}

		return nil
	}

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 2)
	assertCallIDs(t, calls, []string{"github", "cloudflare"})
	assertCallTimes(t, calls, []time.Time{start, start.Add(time.Second)})
	if calls[0].deadline != start.Add(time.Second) {
		t.Fatalf("first deadline = %s, want %s", calls[0].deadline, start.Add(time.Second))
	}
}

func TestSchedulerAdmissionTimeoutSkipsOccurrenceAndContinues(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("github", true),
		schedulerService("cloudflare", true),
	}
	scheduler := newTestScheduler(t, submitter, services, 2*time.Second, clock)

	submitter.onSubmit = func(ctx context.Context, svc service.Service, callNumber int) error {
		if callNumber == 1 {
			deadline, ok := ctx.Deadline()
			if !ok {
				return errors.New("admission context has no deadline")
			}
			clock.AdvanceTo(deadline)
			<-ctx.Done()

			return ctx.Err()
		}

		return nil
	}

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 2)
	assertCallIDs(t, calls, []string{"github", "cloudflare"})
	assertCallTimes(t, calls, []time.Time{start, start.Add(time.Second)})
}

func TestSchedulerLastServiceDeadlineIsNextCycleFirstSlot(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("github", true),
		schedulerService("cloudflare", true),
		schedulerService("openai", true),
	}
	scheduler := newTestScheduler(t, submitter, services, 9*time.Second, clock)

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 3)
	wantDeadlines := []time.Time{
		start.Add(3 * time.Second),
		start.Add(6 * time.Second),
		start.Add(9 * time.Second),
	}
	for index, call := range calls {
		if call.deadline != wantDeadlines[index] {
			t.Fatalf("call %d deadline = %s, want %s", index, call.deadline, wantDeadlines[index])
		}
	}
}

func TestSchedulerSkipsStaleOccurrence(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("github", true),
		schedulerService("cloudflare", true),
		schedulerService("openai", true),
	}
	scheduler := newTestScheduler(t, submitter, services, 3*time.Second, clock)

	submitter.onSubmit = func(ctx context.Context, svc service.Service, callNumber int) error {
		if callNumber == 1 {
			clock.AdvanceTo(start.Add(2400 * time.Millisecond))
		}

		return nil
	}

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 2)
	assertCallIDs(t, calls, []string{"github", "openai"})
	assertCallTimes(t, calls, []time.Time{start, start.Add(2400 * time.Millisecond)})
}

func TestSchedulerSkipsMultipleMissedSlotsWithoutCatchUpBurst(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("github", true),
		schedulerService("cloudflare", true),
		schedulerService("openai", true),
		schedulerService("slack", true),
	}
	scheduler := newTestScheduler(t, submitter, services, 4*time.Second, clock)

	submitter.onSubmit = func(ctx context.Context, svc service.Service, callNumber int) error {
		if callNumber == 1 {
			clock.AdvanceTo(start.Add(10500 * time.Millisecond))
		}

		return nil
	}

	calls := runSchedulerForSubmissions(t, scheduler, submitter, 2)
	assertCallIDs(t, calls, []string{"github", "openai"})
	assertCallTimes(t, calls, []time.Time{start, start.Add(10500 * time.Millisecond)})
}

func TestSchedulerCancellationWhileWaitingStopsPromptly(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	clock.autoSleep = false
	clock.sleepStarted = make(chan time.Time, 1)
	submitter := newRecordingSchedulerSubmitter(clock)
	services := []service.Service{
		schedulerService("github", true),
		schedulerService("cloudflare", true),
	}
	scheduler := newTestScheduler(t, submitter, services, 10*time.Second, clock)
	ctx, cancel := context.WithCancel(context.Background())
	done := runScheduler(t, scheduler, ctx)

	waitSchedulerSleep(t, clock.sleepStarted)
	cancel()

	if err := waitSchedulerResult(t, done); err != nil {
		t.Fatalf("Run() error = %v, want nil after cancellation", err)
	}
}

func TestSchedulerCancellationDuringBlockedSubmissionStopsPromptly(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	blockStarted := make(chan struct{})
	submitter := SubmitterFunc(func(ctx context.Context, svc service.Service) error {
		close(blockStarted)
		<-ctx.Done()

		return ctx.Err()
	})
	scheduler := newTestScheduler(t, submitter, []service.Service{schedulerService("github", true)}, 10*time.Second, clock)
	ctx, cancel := context.WithCancel(context.Background())
	done := runScheduler(t, scheduler, ctx)

	waitClosed(t, blockStarted, "blocked submission")
	cancel()

	if err := waitSchedulerResult(t, done); err != nil {
		t.Fatalf("Run() error = %v, want nil after cancellation", err)
	}
}

func TestSchedulerReturnsUnexpectedSubmitError(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	wantErr := errors.New("worker pool stopped unexpectedly")
	submitter := SubmitterFunc(func(ctx context.Context, svc service.Service) error {
		return wantErr
	})
	scheduler := newTestScheduler(t, submitter, []service.Service{schedulerService("github", true)}, 10*time.Second, clock)

	err := scheduler.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
}

func TestSchedulerReturnsDeadlineErrorUnrelatedToAdmissionWindow(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := SubmitterFunc(func(ctx context.Context, svc service.Service) error {
		return context.DeadlineExceeded
	})
	scheduler := newTestScheduler(t, submitter, []service.Service{schedulerService("github", true)}, 10*time.Second, clock)

	err := scheduler.Run(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestSchedulerSecondRunWhileActiveReturnsError(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	blockStarted := make(chan struct{})
	submitter := SubmitterFunc(func(ctx context.Context, svc service.Service) error {
		close(blockStarted)
		<-ctx.Done()

		return ctx.Err()
	})
	scheduler := newTestScheduler(t, submitter, []service.Service{schedulerService("github", true)}, 10*time.Second, clock)
	ctx, cancel := context.WithCancel(context.Background())
	done := runScheduler(t, scheduler, ctx)
	waitClosed(t, blockStarted, "blocked submission")

	if err := scheduler.Run(context.Background()); !errors.Is(err, errSchedulerAlreadyRun) {
		t.Fatalf("second Run() error = %v, want %v", err, errSchedulerAlreadyRun)
	}

	cancel()
	if err := waitSchedulerResult(t, done); err != nil {
		t.Fatalf("first Run() error = %v, want nil after cancellation", err)
	}
}

func TestSchedulerSecondRunAfterTerminationReturnsError(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeSchedulerClock(start)
	submitter := newRecordingSchedulerSubmitter(clock)
	scheduler := newTestScheduler(t, submitter, []service.Service{schedulerService("github", true)}, 10*time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("first Run() error = %v, want nil", err)
	}

	if err := scheduler.Run(context.Background()); !errors.Is(err, errSchedulerAlreadyRun) {
		t.Fatalf("second Run() error = %v, want %v", err, errSchedulerAlreadyRun)
	}
}

func newTestScheduler(t *testing.T, submitter Submitter, services []service.Service, interval time.Duration, clock schedulerClock) *Scheduler {
	t.Helper()

	scheduler, err := NewScheduler(submitter, services, interval)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	scheduler.clock = clock

	return scheduler
}

func runSchedulerForSubmissions(t *testing.T, scheduler *Scheduler, submitter *recordingSchedulerSubmitter, count int) []schedulerSubmission {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	previousHook := submitter.onSubmit
	submitter.onSubmit = func(ctx context.Context, svc service.Service, callNumber int) error {
		var err error
		if previousHook != nil {
			err = previousHook(ctx, svc, callNumber)
		}

		if callNumber == count {
			cancel()
		}

		return err
	}

	done := runScheduler(t, scheduler, ctx)
	if err := waitSchedulerResult(t, done); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	return submitter.Calls()
}

func runScheduler(t *testing.T, scheduler *Scheduler, ctx context.Context) <-chan error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- scheduler.Run(ctx)
	}()

	return done
}

func waitSchedulerResult(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(workerPoolTestTimeout):
		t.Fatal("timed out waiting for scheduler")
	}

	return nil
}

func waitSchedulerSleep(t *testing.T, sleepStarted <-chan time.Time) time.Time {
	t.Helper()

	select {
	case target := <-sleepStarted:
		return target
	case <-time.After(workerPoolTestTimeout):
		t.Fatal("timed out waiting for scheduler sleep")
	}

	return time.Time{}
}

func assertCallIDs(t *testing.T, calls []schedulerSubmission, want []string) {
	t.Helper()

	if len(calls) != len(want) {
		t.Fatalf("calls = %d, want %d", len(calls), len(want))
	}

	for index, call := range calls {
		if call.serviceID != want[index] {
			t.Fatalf("call %d service = %q, want %q", index, call.serviceID, want[index])
		}
	}
}

func assertCallTimes(t *testing.T, calls []schedulerSubmission, want []time.Time) {
	t.Helper()

	if len(calls) != len(want) {
		t.Fatalf("calls = %d, want %d", len(calls), len(want))
	}

	for index, call := range calls {
		if call.at != want[index] {
			t.Fatalf("call %d target = %s, want %s", index, call.at, want[index])
		}
	}
}

func schedulerServices(count int) []service.Service {
	services := make([]service.Service, 0, count)
	for index := range count {
		services = append(services, schedulerService(fmt.Sprintf("service-%02d", index), true))
	}

	return services
}

func schedulerService(id string, enabled bool) service.Service {
	return service.Service{
		ID:       id,
		CheckURL: "https://example.test/" + id,
		Enabled:  enabled,
	}
}

type SubmitterFunc func(context.Context, service.Service) error

func (fn SubmitterFunc) Submit(ctx context.Context, svc service.Service) error {
	return fn(ctx, svc)
}

type schedulerSubmission struct {
	serviceID string
	at        time.Time
	deadline  time.Time
}

type recordingSchedulerSubmitter struct {
	clock    *fakeSchedulerClock
	onSubmit func(context.Context, service.Service, int) error

	mu    sync.Mutex
	calls []schedulerSubmission
}

func newRecordingSchedulerSubmitter(clock *fakeSchedulerClock) *recordingSchedulerSubmitter {
	return &recordingSchedulerSubmitter{clock: clock}
}

func (s *recordingSchedulerSubmitter) Submit(ctx context.Context, svc service.Service) error {
	deadline, _ := ctx.Deadline()

	s.mu.Lock()
	s.calls = append(s.calls, schedulerSubmission{
		serviceID: svc.ID,
		at:        s.clock.Now(),
		deadline:  deadline,
	})
	callNumber := len(s.calls)
	s.mu.Unlock()

	if s.onSubmit != nil {
		return s.onSubmit(ctx, svc, callNumber)
	}

	return nil
}

func (s *recordingSchedulerSubmitter) Calls() []schedulerSubmission {
	s.mu.Lock()
	defer s.mu.Unlock()

	calls := make([]schedulerSubmission, len(s.calls))
	copy(calls, s.calls)

	return calls
}

type fakeSchedulerClock struct {
	mu           sync.Mutex
	now          time.Time
	autoSleep    bool
	sleepStarted chan time.Time
	sleepers     []fakeSchedulerSleeper
	deadlines    []*fakeDeadlineContext
}

type fakeSchedulerSleeper struct {
	target time.Time
	done   chan struct{}
}

func newFakeSchedulerClock(now time.Time) *fakeSchedulerClock {
	return &fakeSchedulerClock{
		now:       now,
		autoSleep: true,
	}
}

func (c *fakeSchedulerClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeSchedulerClock) SleepUntil(ctx context.Context, target time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	if !c.now.Before(target) {
		c.mu.Unlock()
		return ctx.Err()
	}

	if c.autoSleep {
		c.advanceToLocked(target)
		c.mu.Unlock()
		return ctx.Err()
	}

	done := make(chan struct{})
	c.sleepers = append(c.sleepers, fakeSchedulerSleeper{target: target, done: done})
	if c.sleepStarted != nil {
		select {
		case c.sleepStarted <- target:
		default:
		}
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return ctx.Err()
	}
}

func (c *fakeSchedulerClock) WithDeadline(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	deadlineContext := newFakeDeadlineContext(ctx, deadline)

	c.mu.Lock()
	if !c.now.Before(deadline) {
		deadlineContext.expire()
	} else {
		c.deadlines = append(c.deadlines, deadlineContext)
	}
	c.mu.Unlock()

	return deadlineContext, func() {
		deadlineContext.cancel(context.Canceled)
	}
}

func (c *fakeSchedulerClock) AdvanceBy(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.advanceToLocked(c.now.Add(duration))
}

func (c *fakeSchedulerClock) AdvanceTo(target time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if target.After(c.now) {
		c.advanceToLocked(target)
	}
}

func (c *fakeSchedulerClock) advanceToLocked(target time.Time) {
	c.now = target
	c.expireDeadlinesLocked()
	c.releaseSleepersLocked()
}

func (c *fakeSchedulerClock) expireDeadlinesLocked() {
	for _, deadline := range c.deadlines {
		if !c.now.Before(deadline.deadline) {
			deadline.expire()
		}
	}
}

func (c *fakeSchedulerClock) releaseSleepersLocked() {
	sleepers := c.sleepers[:0]
	for _, sleeper := range c.sleepers {
		if c.now.Before(sleeper.target) {
			sleepers = append(sleepers, sleeper)
			continue
		}

		close(sleeper.done)
	}
	c.sleepers = sleepers
}

type fakeDeadlineContext struct {
	parent   context.Context
	deadline time.Time
	done     chan struct{}
	once     sync.Once

	mu  sync.Mutex
	err error
}

func newFakeDeadlineContext(parent context.Context, deadline time.Time) *fakeDeadlineContext {
	ctx := &fakeDeadlineContext{
		parent:   parent,
		deadline: deadline,
		done:     make(chan struct{}),
	}

	go func() {
		select {
		case <-parent.Done():
			ctx.cancel(parent.Err())
		case <-ctx.done:
		}
	}()

	return ctx
}

func (c *fakeDeadlineContext) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func (c *fakeDeadlineContext) Done() <-chan struct{} {
	return c.done
}

func (c *fakeDeadlineContext) Err() error {
	select {
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()

		return c.err
	default:
		return nil
	}
}

func (c *fakeDeadlineContext) Value(key any) any {
	return c.parent.Value(key)
}

func (c *fakeDeadlineContext) expire() {
	c.cancel(context.DeadlineExceeded)
}

func (c *fakeDeadlineContext) cancel(err error) {
	if err == nil {
		err = context.Canceled
	}

	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
	})
}
