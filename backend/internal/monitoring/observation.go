package monitoring

import "time"

type ObservationKind string

const (
	ObservationHealthy       ObservationKind = "healthy"
	ObservationFailure       ObservationKind = "failure"
	ObservationIndeterminate ObservationKind = "indeterminate"
	ObservationIgnored       ObservationKind = "ignored"
)

type Observation struct {
	ServiceID  string
	ObservedAt time.Time
	Kind       ObservationKind
}
