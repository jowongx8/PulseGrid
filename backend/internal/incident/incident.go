package incident

import (
	"errors"
	"fmt"
	"time"

	"github.com/jowongx8/backend/internal/status"
)

var (
	ErrInvalidIncident  = errors.New("incident: invalid incident")
	ErrInvalidUpdate    = errors.New("incident: invalid status update")
	ErrNoActiveIncident = errors.New("incident: no active incident")
)

// Incident represents one outage. A nil ResolvedAt means it is still open.
type Incident struct {
	ServiceID  string
	StartedAt  time.Time
	ResolvedAt *time.Time
}

// Lifecycle tracks the currently open incident for each service.
type Lifecycle struct {
	active map[string]Incident
}

func NewLifecycle() *Lifecycle {
	return &Lifecycle{active: make(map[string]Incident)}
}

// Active returns an independent snapshot of a service's open incident.
func (l *Lifecycle) Active(serviceID string) (Incident, bool) {
	incident, ok := l.active[serviceID]
	if !ok {
		return Incident{}, false
	}
	return copyIncident(incident), true
}

// RestoreActive reconstructs an unresolved incident that predates this process.
func (l *Lifecycle) RestoreActive(value Incident) error {
	if value.ServiceID == "" {
		return fmt.Errorf("%w: service ID is empty", ErrInvalidIncident)
	}
	if value.StartedAt.IsZero() {
		return fmt.Errorf("%w: service %q has no start time", ErrInvalidIncident, value.ServiceID)
	}
	if value.ResolvedAt != nil {
		return fmt.Errorf("%w: service %q is already resolved", ErrInvalidIncident, value.ServiceID)
	}
	if _, exists := l.active[value.ServiceID]; exists {
		return fmt.Errorf("%w: service %q already has an active incident", ErrInvalidIncident, value.ServiceID)
	}

	l.active[value.ServiceID] = copyIncident(value)
	return nil
}

// Apply opens or resolves an incident for a confirmed public status transition.
// It returns nil when the update has no incident action.
func (l *Lifecycle) Apply(update status.Update) (*Incident, error) {
	if !update.Applied || !update.Changed {
		return nil, nil
	}
	if err := validateTransition(update); err != nil {
		return nil, err
	}

	switch update.Current {
	case status.StatusDown:
		if _, exists := l.active[update.ServiceID]; exists {
			return nil, nil
		}
		opened := Incident{ServiceID: update.ServiceID, StartedAt: update.ObservedAt}
		l.active[update.ServiceID] = opened
		result := copyIncident(opened)
		return &result, nil
	case status.StatusUp:
		active, exists := l.active[update.ServiceID]
		if !exists {
			if update.Previous == status.StatusDown {
				return nil, fmt.Errorf("%w for service %q", ErrNoActiveIncident, update.ServiceID)
			}
			return nil, nil
		}
		if !update.ObservedAt.After(active.StartedAt) {
			return nil, fmt.Errorf("%w: service %q resolved at %s, started at %s", ErrInvalidUpdate, update.ServiceID, update.ObservedAt, active.StartedAt)
		}
		resolvedAt := update.ObservedAt
		active.ResolvedAt = &resolvedAt
		delete(l.active, update.ServiceID)
		result := copyIncident(active)
		return &result, nil
	default:
		return nil, nil
	}
}

func validateTransition(update status.Update) error {
	if update.ServiceID == "" {
		return fmt.Errorf("%w: service ID is empty", ErrInvalidUpdate)
	}
	if update.ObservedAt.IsZero() {
		return fmt.Errorf("%w: service %q has no observation time", ErrInvalidUpdate, update.ServiceID)
	}
	if !validStatus(update.Previous) || !validStatus(update.Current) || update.Previous == update.Current {
		return fmt.Errorf("%w: service %q transition %q to %q", ErrInvalidUpdate, update.ServiceID, update.Previous, update.Current)
	}
	return nil
}

func validStatus(value status.ServiceStatus) bool {
	switch value {
	case status.StatusUnknown, status.StatusUp, status.StatusDown:
		return true
	default:
		return false
	}
}

func copyIncident(incident Incident) Incident {
	if incident.ResolvedAt != nil {
		resolvedAt := *incident.ResolvedAt
		incident.ResolvedAt = &resolvedAt
	}
	return incident
}
