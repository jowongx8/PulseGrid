package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
)

var errRuntimeAlreadyRun = errors.New("monitoring runtime already ran")

// MonitoringRuntime coordinates one in-memory monitoring pipeline.
type MonitoringRuntime struct {
	pool      *monitoring.WorkerPool
	scheduler *monitoring.Scheduler
	tracker   *status.Tracker
	submitter *outstandingSubmitter
	started   atomic.Bool
}

type outstandingSubmitter struct {
	delegate    monitoring.Submitter
	mu          sync.Mutex
	outstanding map[string]struct{}
}

func NewMonitoringRuntime(
	services []service.Service,
	checker monitoring.Checker,
	interval time.Duration,
	workerCount int,
	queueCapacity int,
) (*MonitoringRuntime, error) {
	snapshot := append([]service.Service(nil), services...)

	tracker, err := status.NewTracker(snapshot)
	if err != nil {
		return nil, fmt.Errorf("create status tracker: %w", err)
	}

	pool, err := monitoring.NewWorkerPool(checker, workerCount, queueCapacity)
	if err != nil {
		return nil, fmt.Errorf("create worker pool: %w", err)
	}

	submitter := &outstandingSubmitter{
		delegate:    pool,
		outstanding: make(map[string]struct{}),
	}
	scheduler, err := monitoring.NewScheduler(submitter, snapshot, interval)
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}

	return &MonitoringRuntime{
		pool:      pool,
		scheduler: scheduler,
		tracker:   tracker,
		submitter: submitter,
	}, nil
}

func (r *MonitoringRuntime) Tracker() *status.Tracker {
	return r.tracker
}

// Run blocks until cancellation or failure. A runtime can only be run once.
func (r *MonitoringRuntime) Run(ctx context.Context) error {
	if !r.started.CompareAndSwap(false, true) {
		return errRuntimeAlreadyRun
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type exit struct {
		component string
		err       error
	}
	exits := make(chan exit, 3)
	go func() {
		exits <- exit{component: "result consumer", err: processResults(runCtx, r.pool.Results(), r.tracker, r.submitter)}
	}()
	go func() {
		r.pool.Run(runCtx)
		exits <- exit{component: "worker pool"}
	}()
	go func() {
		exits <- exit{component: "scheduler", err: r.scheduler.Run(runCtx)}
	}()

	var firstErr error
	callerDone := ctx.Done()
	for remaining := 3; remaining > 0; {
		select {
		case <-callerDone:
			callerDone = nil
			cancel()
		case result := <-exits:
			remaining--
			if result.err == nil && runCtx.Err() == nil {
				result.err = errors.New("stopped unexpectedly")
			}
			if result.err != nil && firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", result.component, result.err)
				cancel()
			}
		}
	}
	return firstErr
}

func (s *outstandingSubmitter) Submit(ctx context.Context, svc service.Service) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if _, exists := s.outstanding[svc.ID]; exists {
		s.mu.Unlock()
		return nil
	}
	s.outstanding[svc.ID] = struct{}{}
	s.mu.Unlock()

	if err := s.delegate.Submit(ctx, svc); err != nil {
		s.complete(svc.ID)
		return err
	}
	return nil
}

func (s *outstandingSubmitter) complete(serviceID string) {
	s.mu.Lock()
	delete(s.outstanding, serviceID)
	s.mu.Unlock()
}

func processResults(
	ctx context.Context,
	results <-chan monitoring.CheckResult,
	tracker *status.Tracker,
	submitter *outstandingSubmitter,
) error {
	for result := range results {
		observation := monitoring.Evaluate(result)
		if _, err := tracker.Apply(observation); err != nil {
			return fmt.Errorf("apply observation for service %q: %w", result.ServiceID, err)
		}
		submitter.complete(result.ServiceID)
	}
	if ctx.Err() == nil {
		return errors.New("results channel closed while monitoring active")
	}
	return nil
}
