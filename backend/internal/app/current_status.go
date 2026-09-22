package app

import (
	"errors"
	"time"

	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
)

type ServiceStatusView struct {
	ID         string
	Name       string
	Category   service.Category
	WebsiteURL string
	Weight     int
	Status     CurrentStatusView
}

type CurrentStatusView struct {
	State         status.ServiceStatus
	LastCheckedAt *time.Time
}

// StatusSnapshotReader provides the current monitoring-derived service states.
type StatusSnapshotReader interface {
	Snapshot() status.Snapshot
}

// CurrentStatusQuery composes configured service metadata with current status.
type CurrentStatusQuery struct {
	services []service.Service
	statuses StatusSnapshotReader
}

func NewCurrentStatusQuery(services []service.Service, statuses StatusSnapshotReader) (*CurrentStatusQuery, error) {
	if statuses == nil {
		return nil, errors.New("status snapshot reader is required")
	}

	enabledServices := make([]service.Service, 0, len(services))
	for _, svc := range services {
		if svc.Enabled {
			enabledServices = append(enabledServices, svc)
		}
	}

	return &CurrentStatusQuery{
		services: enabledServices,
		statuses: statuses,
	}, nil
}

func (q *CurrentStatusQuery) Execute() []ServiceStatusView {
	snapshot := q.statuses.Snapshot()
	views := make([]ServiceStatusView, 0, len(q.services))

	for _, svc := range q.services {
		current := CurrentStatusView{State: status.StatusUnknown}
		if entry, observed := snapshot[svc.ID]; observed {
			lastCheckedAt := entry.LastCheckedAt
			current.State = entry.State
			current.LastCheckedAt = &lastCheckedAt
		}

		views = append(views, ServiceStatusView{
			ID:         svc.ID,
			Name:       svc.Name,
			Category:   svc.Category,
			WebsiteURL: svc.WebsiteURL,
			Weight:     svc.Weight,
			Status:     current,
		})
	}

	return views
}
