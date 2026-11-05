package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jowongx8/backend/internal/event"
	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
)

var errRuntimeAlreadyRun = errors.New("monitoring runtime already ran")

// CheckResultWriter persists raw monitoring results before they affect status.
type CheckResultWriter interface {
	Save(context.Context, monitoring.CheckResult) error
}

// EventPublisher publishes meaningful application state changes.
type EventPublisher interface {
	Publish(event.Event) error
}

// MonitoringRuntime coordinates one monitoring pipeline.
type MonitoringRuntime struct {
	pool      *monitoring.WorkerPool
	scheduler *monitoring.Scheduler
	tracker   *status.Tracker
	writer    CheckResultWriter
	incidents *IncidentProcessor
	publisher EventPublisher
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
	writer CheckResultWriter,
	incidents *IncidentProcessor,
	publisher EventPublisher,
	interval time.Duration,
	workerCount int,
	queueCapacity int,
) (*MonitoringRuntime, error) {
	if writer == nil {
		return nil, errors.New("check result writer is required")
	}
	if incidents == nil {
		return nil, errors.New("incident processor is required")
	}
	if publisher == nil {
		return nil, errors.New("event publisher is required")
	}

	snapshot := append([]service.Service(nil), services...)
	serviceIDs := make([]string, 0, len(snapshot))
	for _, svc := range snapshot {
		if svc.Enabled {
			serviceIDs = append(serviceIDs, svc.ID)
		}
	}

	tracker, err := status.NewTracker(serviceIDs)
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
		writer:    writer,
		incidents: incidents,
		publisher: publisher,
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
		exits <- exit{component: "result consumer", err: processResults(runCtx, r.pool.Results(), r.writer, r.tracker, r.incidents, r.publisher, r.submitter)}
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
	writer CheckResultWriter,
	tracker *status.Tracker,
	incidents *IncidentProcessor,
	publisher EventPublisher,
	submitter *outstandingSubmitter,
) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case result, ok := <-results:
			if ctx.Err() != nil {
				return nil
			}
			if !ok {
				return errors.New("results channel closed while monitoring active")
			}
			if err := writer.Save(ctx, result); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
					return nil
				}
				return fmt.Errorf("save check result for service %q: %w", result.ServiceID, err)
			}
			observation := monitoring.Evaluate(result)
			update, err := tracker.Apply(observation)
			if err != nil {
				return fmt.Errorf("apply observation for service %q: %w", result.ServiceID, err)
			}
			transition, err := incidents.Process(ctx, update)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
					return nil
				}
				return fmt.Errorf("process incident update for service %q: %w", result.ServiceID, err)
			}
			if err := publishEvents(publisher, update, transition); err != nil {
				return err
			}
			submitter.complete(result.ServiceID)
		}
	}
}

func publishEvents(publisher EventPublisher, update status.Update, transition *incident.Incident) error {
	if update.Applied && update.Changed {
		if err := publisher.Publish(event.StatusChanged{
			ServiceID:     update.ServiceID,
			State:         update.Current,
			LastCheckedAt: update.ObservedAt,
		}); err != nil {
			return fmt.Errorf("publish status changed event for service %q: %w", update.ServiceID, err)
		}
	}

	if transition == nil {
		return nil
	}
	if transition.ResolvedAt == nil {
		if err := publisher.Publish(event.IncidentOpened{
			ServiceID: transition.ServiceID,
			StartedAt: transition.StartedAt,
		}); err != nil {
			return fmt.Errorf("publish incident opened event for service %q: %w", transition.ServiceID, err)
		}
		return nil
	}

	if err := publisher.Publish(event.IncidentResolved{
		ServiceID:  transition.ServiceID,
		StartedAt:  transition.StartedAt,
		ResolvedAt: *transition.ResolvedAt,
	}); err != nil {
		return fmt.Errorf("publish incident resolved event for service %q: %w", transition.ServiceID, err)
	}
	return nil
}
