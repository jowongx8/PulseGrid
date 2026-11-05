package event

import (
	"time"

	"github.com/jowongx8/backend/internal/status"
)

// Event is a transport-neutral notification about a meaningful public state
// change.
type Event interface {
	event()
}

type StatusChanged struct {
	ServiceID     string
	State         status.ServiceStatus
	LastCheckedAt time.Time
}

func (StatusChanged) event() {}

type IncidentOpened struct {
	ServiceID string
	StartedAt time.Time
}

func (IncidentOpened) event() {}

type IncidentResolved struct {
	ServiceID  string
	StartedAt  time.Time
	ResolvedAt time.Time
}

func (IncidentResolved) event() {}
