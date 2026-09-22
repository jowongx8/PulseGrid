package app

import (
	"context"
	"fmt"

	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/status"
)

// IncidentReader loads unresolved incidents needed to initialize the lifecycle.
type IncidentReader interface {
	ListOpen(context.Context) ([]incident.Incident, error)
}

// RestoreOpenIncidents reconstructs lifecycle state before live monitoring starts.
func RestoreOpenIncidents(ctx context.Context, lifecycle *incident.Lifecycle, reader IncidentReader) error {
	values, err := reader.ListOpen(ctx)
	if err != nil {
		return fmt.Errorf("list open incidents: %w", err)
	}
	for _, value := range values {
		if err := lifecycle.RestoreActive(value); err != nil {
			return fmt.Errorf("restore open incident for service %q: %w", value.ServiceID, err)
		}
	}
	return nil
}

// IncidentWriter persists incident openings and resolutions.
type IncidentWriter interface {
	Open(context.Context, incident.Incident) error
	Resolve(context.Context, incident.Incident) error
}

// IncidentProcessor applies confirmed status updates to the live incident lifecycle.
type IncidentProcessor struct {
	lifecycle *incident.Lifecycle
	writer    IncidentWriter
}

func NewIncidentProcessor(lifecycle *incident.Lifecycle, writer IncidentWriter) *IncidentProcessor {
	return &IncidentProcessor{lifecycle: lifecycle, writer: writer}
}

func (p *IncidentProcessor) Process(ctx context.Context, update status.Update) (*incident.Incident, error) {
	value, err := p.lifecycle.Apply(update)
	if err != nil {
		return nil, fmt.Errorf("apply incident update for service %q: %w", update.ServiceID, err)
	}
	if value == nil {
		return nil, nil
	}

	transition := copyIncident(*value)
	if transition.ResolvedAt == nil {
		if err := p.writer.Open(ctx, copyIncident(transition)); err != nil {
			return nil, fmt.Errorf("open incident for service %q: %w", transition.ServiceID, err)
		}
		return &transition, nil
	}
	if err := p.writer.Resolve(ctx, copyIncident(transition)); err != nil {
		return nil, fmt.Errorf("resolve incident for service %q: %w", transition.ServiceID, err)
	}
	return &transition, nil
}

func copyIncident(value incident.Incident) incident.Incident {
	if value.ResolvedAt != nil {
		resolvedAt := *value.ResolvedAt
		value.ResolvedAt = &resolvedAt
	}
	return value
}
