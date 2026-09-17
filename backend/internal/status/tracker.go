package status

import (
	"fmt"
	"sync"
	"time"

	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
)

const (
	failureThreshold       = 3
	recoveryThreshold      = 2
	indeterminateThreshold = 3
)

// Tracker maintains current public service statuses from monitoring observations.
//
// Tracker is safe for concurrent use. Apply is atomic for each observation, but
// the tracker does not reconstruct chronological order for arbitrarily
// out-of-order concurrent observations of the same service. Normal runtime
// integration should deliver observations for a service in event-time order.
type Tracker struct {
	mu     sync.RWMutex
	order  []string
	states map[string]*serviceState
}

type serviceState struct {
	status              ServiceStatus
	lastObservedAt      time.Time
	statusChangedAt     time.Time
	failureStreak       int
	healthyStreak       int
	indeterminateStreak int
}

func NewTracker(services []service.Service) (*Tracker, error) {
	tracker := &Tracker{
		order:  make([]string, 0, len(services)),
		states: make(map[string]*serviceState, len(services)),
	}

	for index, svc := range services {
		if !svc.Enabled {
			continue
		}

		if svc.ID == "" {
			return nil, fmt.Errorf("%w: service at index %d", ErrInvalidServiceID, index)
		}

		if _, exists := tracker.states[svc.ID]; exists {
			return nil, fmt.Errorf("%w: service %q", ErrDuplicateServiceID, svc.ID)
		}

		tracker.order = append(tracker.order, svc.ID)
		tracker.states[svc.ID] = &serviceState{status: StatusUnknown}
	}

	if len(tracker.order) == 0 {
		return nil, ErrNoEnabledServices
	}

	return tracker, nil
}

func (t *Tracker) Apply(observation monitoring.Observation) (Update, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.states[observation.ServiceID]
	if !exists {
		return Update{}, fmt.Errorf("%w: service %q", ErrUnknownService, observation.ServiceID)
	}

	if observation.ObservedAt.IsZero() {
		return Update{}, fmt.Errorf("%w: service %q", ErrInvalidObservationTime, observation.ServiceID)
	}

	if !validObservationKind(observation.Kind) {
		return Update{}, fmt.Errorf("%w: %q", ErrInvalidObservationKind, observation.Kind)
	}

	previous := state.status
	update := Update{
		ServiceID:  observation.ServiceID,
		Previous:   previous,
		Current:    previous,
		ObservedAt: observation.ObservedAt,
	}

	if !state.lastObservedAt.IsZero() && !observation.ObservedAt.After(state.lastObservedAt) {
		return update, nil
	}

	if observation.Kind == monitoring.ObservationIgnored {
		return update, nil
	}

	state.apply(observation.Kind)
	state.lastObservedAt = observation.ObservedAt
	if state.status != previous {
		state.statusChangedAt = observation.ObservedAt
	}

	update.Current = state.status
	update.Changed = update.Previous != update.Current
	update.Applied = true

	return update, nil
}

func (t *Tracker) Get(serviceID string) (ServiceSnapshot, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.states[serviceID]
	if !exists {
		return ServiceSnapshot{}, fmt.Errorf("%w: service %q", ErrUnknownService, serviceID)
	}

	return snapshotFor(serviceID, state), nil
}

func (t *Tracker) Snapshot() []ServiceSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	snapshots := make([]ServiceSnapshot, 0, len(t.order))
	for _, serviceID := range t.order {
		snapshots = append(snapshots, snapshotFor(serviceID, t.states[serviceID]))
	}

	return snapshots
}

func (s *serviceState) apply(kind monitoring.ObservationKind) {
	switch kind {
	case monitoring.ObservationHealthy:
		s.healthyStreak++
		s.failureStreak = 0
		s.indeterminateStreak = 0

		switch s.status {
		case StatusUnknown:
			s.status = StatusUp
		case StatusDown:
			if s.healthyStreak >= recoveryThreshold {
				s.status = StatusUp
			}
		}
	case monitoring.ObservationFailure:
		s.failureStreak++
		s.healthyStreak = 0
		s.indeterminateStreak = 0

		if s.failureStreak >= failureThreshold {
			s.status = StatusDown
		}
	case monitoring.ObservationIndeterminate:
		s.indeterminateStreak++
		s.failureStreak = 0
		s.healthyStreak = 0

		if s.status != StatusUnknown && s.indeterminateStreak >= indeterminateThreshold {
			s.status = StatusUnknown
		}
	}
}

func validObservationKind(kind monitoring.ObservationKind) bool {
	switch kind {
	case monitoring.ObservationHealthy,
		monitoring.ObservationFailure,
		monitoring.ObservationIndeterminate,
		monitoring.ObservationIgnored:
		return true
	default:
		return false
	}
}

func snapshotFor(serviceID string, state *serviceState) ServiceSnapshot {
	return ServiceSnapshot{
		ServiceID:       serviceID,
		Status:          state.status,
		LastObservedAt:  state.lastObservedAt,
		StatusChangedAt: state.statusChangedAt,
	}
}
