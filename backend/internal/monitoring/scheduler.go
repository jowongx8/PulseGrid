package monitoring

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jowongx8/backend/internal/service"
)

type Submitter interface {
	Submit(context.Context, service.Service) error
}

var _ Submitter = (*WorkerPool)(nil)

var errSchedulerAlreadyRun = errors.New("scheduler already ran")

type Scheduler struct {
	submitter Submitter
	services  []service.Service
	interval  time.Duration
	clock     schedulerClock

	lifecycleMu sync.Mutex
	started     bool
}

type schedulerClock interface {
	Now() time.Time
	SleepUntil(context.Context, time.Time) error
	WithDeadline(context.Context, time.Time) (context.Context, context.CancelFunc)
}

type realSchedulerClock struct{}

func (realSchedulerClock) Now() time.Time {
	return time.Now()
}

func (realSchedulerClock) SleepUntil(ctx context.Context, target time.Time) error {
	if !time.Now().Before(target) {
		return nil
	}

	timer := time.NewTimer(time.Until(target))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (realSchedulerClock) WithDeadline(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ctx, deadline)
}

func NewScheduler(submitter Submitter, services []service.Service, interval time.Duration) (*Scheduler, error) {
	if submitter == nil {
		return nil, errors.New("submitter is required")
	}

	if interval <= 0 {
		return nil, errors.New("interval must be positive")
	}

	enabledServices := enabledServiceSnapshot(services)
	if len(enabledServices) == 0 {
		return nil, errors.New("at least one enabled service is required")
	}

	return &Scheduler{
		submitter: submitter,
		services:  enabledServices,
		interval:  interval,
		clock:     realSchedulerClock{},
	}, nil
}

func enabledServiceSnapshot(services []service.Service) []service.Service {
	enabledServices := make([]service.Service, 0, len(services))
	for _, svc := range services {
		if svc.Enabled {
			enabledServices = append(enabledServices, svc)
		}
	}

	return enabledServices
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.lifecycleMu.Lock()
	if s.started {
		s.lifecycleMu.Unlock()
		return errSchedulerAlreadyRun
	}
	s.started = true
	s.lifecycleMu.Unlock()

	return s.run(ctx)
}

func (s *Scheduler) run(ctx context.Context) error {
	base := s.clock.Now()
	var slot int64

	for {
		if ctx.Err() != nil {
			return nil
		}

		now := s.clock.Now()
		for !now.Before(s.targetForSlot(base, slot+1)) {
			slot++
		}

		target := s.targetForSlot(base, slot)
		if now.Before(target) {
			if err := s.clock.SleepUntil(ctx, target); err != nil {
				if ctx.Err() != nil {
					return nil
				}

				return err
			}

			continue
		}

		if err := s.submitSlot(ctx, base, slot); err != nil {
			return err
		}
		slot++
	}
}

func (s *Scheduler) submitSlot(ctx context.Context, base time.Time, slot int64) error {
	deadline := s.targetForSlot(base, slot+1)
	admissionContext, cancel := s.clock.WithDeadline(ctx, deadline)
	defer cancel()

	svc := s.services[int(slot%int64(len(s.services)))]
	err := s.submitter.Submit(admissionContext, svc)
	if err == nil {
		return nil
	}

	if ctx.Err() != nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) && errors.Is(admissionContext.Err(), context.DeadlineExceeded) {
		return nil
	}

	return err
}

func (s *Scheduler) targetForSlot(base time.Time, slot int64) time.Time {
	serviceCount := int64(len(s.services))
	cycle := slot / serviceCount
	serviceIndex := slot % serviceCount

	return base.Add(time.Duration(cycle) * s.interval).Add(s.offsetForService(int(serviceIndex)))
}

func (s *Scheduler) offsetForService(index int) time.Duration {
	return s.interval * time.Duration(index) / time.Duration(len(s.services))
}
