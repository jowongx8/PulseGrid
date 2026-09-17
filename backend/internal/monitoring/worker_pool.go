package monitoring

import (
	"context"
	"errors"
	"sync"

	"github.com/jowongx8/backend/internal/service"
)

type Checker interface {
	Check(context.Context, service.Service) CheckResult
}

var _ Checker = (*HTTPChecker)(nil)

var errWorkerPoolStopped = errors.New("worker pool has stopped")

// WorkerPool executes monitoring checks with a fixed worker count.
//
// A WorkerPool is single-run: the first Run call owns the worker lifecycle, and
// later Run calls return without starting another set of workers.
type WorkerPool struct {
	checker     Checker
	workerCount int
	jobs        chan service.Service
	results     chan CheckResult
	stopping    chan struct{}
	done        chan struct{}
	lifecycleMu sync.Mutex
	started     bool
}

func NewWorkerPool(checker Checker, workerCount int, queueCapacity int) (*WorkerPool, error) {
	if checker == nil {
		return nil, errors.New("checker is required")
	}

	if workerCount <= 0 {
		return nil, errors.New("worker count must be positive")
	}

	if queueCapacity <= 0 {
		return nil, errors.New("queue capacity must be positive")
	}

	return &WorkerPool{
		checker:     checker,
		workerCount: workerCount,
		jobs:        make(chan service.Service, queueCapacity),
		results:     make(chan CheckResult, workerCount),
		stopping:    make(chan struct{}),
		done:        make(chan struct{}),
	}, nil
}

func (p *WorkerPool) Run(ctx context.Context) {
	p.lifecycleMu.Lock()
	if p.started {
		p.lifecycleMu.Unlock()
		return
	}
	p.started = true
	p.lifecycleMu.Unlock()

	p.run(ctx)
}

func (p *WorkerPool) run(ctx context.Context) {
	defer close(p.done)

	var workers sync.WaitGroup
	workers.Add(p.workerCount)

	stoppingClosed := make(chan struct{})
	go func() {
		defer close(stoppingClosed)
		<-ctx.Done()
		close(p.stopping)
	}()

	for range p.workerCount {
		go func() {
			defer workers.Done()
			p.runWorker(ctx)
		}()
	}

	workers.Wait()
	<-stoppingClosed
	close(p.results)
}

func (p *WorkerPool) Submit(ctx context.Context, svc service.Service) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case <-p.done:
		return errWorkerPoolStopped
	case <-p.stopping:
		return errWorkerPoolStopped
	default:
	}

	select {
	case p.jobs <- svc:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return errWorkerPoolStopped
	case <-p.stopping:
		return errWorkerPoolStopped
	}
}

func (p *WorkerPool) Results() <-chan CheckResult {
	return p.results
}

func (p *WorkerPool) runWorker(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case svc := <-p.jobs:
			if ctx.Err() != nil {
				return
			}

			result := p.checker.Check(ctx, svc)

			select {
			case p.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}
