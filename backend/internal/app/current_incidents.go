package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/service"
)

const publicIncidentTarget = 20

// IncidentFeedReader filters membership before computing feed capacity. An
// empty service list returns no incidents, including retained historical rows.
type IncidentFeedReader interface {
	ListFeed(context.Context, int, []string) (active []incident.Incident, resolved []incident.Incident, err error)
}

type IncidentView struct {
	ServiceID  string
	StartedAt  time.Time
	ResolvedAt *time.Time
}

type IncidentsView struct {
	Active   []IncidentView
	Resolved []IncidentView
}

type IncidentsQuery struct {
	incidents  IncidentFeedReader
	serviceIDs []string
}

func NewIncidentsQuery(incidents IncidentFeedReader, services []service.Service) (*IncidentsQuery, error) {
	if incidents == nil {
		return nil, errors.New("incident feed reader is required")
	}
	serviceIDs := make([]string, 0, len(services))
	for _, svc := range services {
		if svc.Enabled {
			serviceIDs = append(serviceIDs, svc.ID)
		}
	}
	return &IncidentsQuery{incidents: incidents, serviceIDs: serviceIDs}, nil
}

func (q *IncidentsQuery) Execute(ctx context.Context) (IncidentsView, error) {
	active, resolved, err := q.incidents.ListFeed(ctx, publicIncidentTarget, q.serviceIDs)
	if err != nil {
		return IncidentsView{}, fmt.Errorf("list incident feed: %w", err)
	}

	return IncidentsView{
		Active:   incidentViews(active),
		Resolved: incidentViews(resolved),
	}, nil
}

func incidentViews(values []incident.Incident) []IncidentView {
	views := make([]IncidentView, 0, len(values))
	for _, value := range values {
		view := IncidentView{
			ServiceID: value.ServiceID,
			StartedAt: value.StartedAt,
		}
		if value.ResolvedAt != nil {
			resolvedAt := *value.ResolvedAt
			view.ResolvedAt = &resolvedAt
		}
		views = append(views, view)
	}
	return views
}
