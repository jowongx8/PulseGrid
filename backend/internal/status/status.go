package status

import (
	"errors"
	"time"
)

type ServiceStatus string

const (
	StatusUnknown ServiceStatus = "unknown"
	StatusUp      ServiceStatus = "up"
	StatusDown    ServiceStatus = "down"
)

var (
	ErrNoEnabledServices      = errors.New("status: no enabled services")
	ErrInvalidServiceID       = errors.New("status: invalid service ID")
	ErrDuplicateServiceID     = errors.New("status: duplicate service ID")
	ErrUnknownService         = errors.New("status: unknown service")
	ErrInvalidObservationKind = errors.New("status: invalid observation kind")
	ErrInvalidObservationTime = errors.New("status: invalid observation time")
)

type Update struct {
	ServiceID  string
	Previous   ServiceStatus
	Current    ServiceStatus
	Changed    bool
	ObservedAt time.Time
	Applied    bool
}

type ServiceSnapshot struct {
	ServiceID       string
	Status          ServiceStatus
	LastObservedAt  time.Time
	StatusChangedAt time.Time
}
