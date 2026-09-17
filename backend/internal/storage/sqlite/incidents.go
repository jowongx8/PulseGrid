package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jowongx8/backend/internal/incident"
)

// IncidentStore persists incident openings and resolutions.
type IncidentStore struct {
	db *sql.DB
}

func NewIncidentStore(db *sql.DB) *IncidentStore {
	return &IncidentStore{db: db}
}

func (s *IncidentStore) Open(ctx context.Context, value incident.Incident) error {
	if value.ServiceID == "" {
		return errors.New("open incident: service ID must not be empty")
	}
	if value.StartedAt.IsZero() {
		return fmt.Errorf("open incident for service %q: start time must not be zero", value.ServiceID)
	}
	if value.ResolvedAt != nil {
		return fmt.Errorf("open incident for service %q: resolution time must be nil", value.ServiceID)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO incidents (service_id, started_at_ms, resolved_at_ms)
		VALUES (?, ?, NULL)
	`, value.ServiceID, value.StartedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("open incident for service %q: %w", value.ServiceID, err)
	}
	return nil
}

func (s *IncidentStore) Resolve(ctx context.Context, value incident.Incident) error {
	if value.ServiceID == "" {
		return errors.New("resolve incident: service ID must not be empty")
	}
	if value.StartedAt.IsZero() {
		return fmt.Errorf("resolve incident for service %q: start time must not be zero", value.ServiceID)
	}
	if value.ResolvedAt == nil {
		return fmt.Errorf("resolve incident for service %q: resolution time is required", value.ServiceID)
	}
	if !value.ResolvedAt.After(value.StartedAt) {
		return fmt.Errorf("resolve incident for service %q: resolution time must be after start time", value.ServiceID)
	}

	// Times in the same stored millisecond are left unchanged; the table CHECK
	// rejects a resolution that cannot be represented after its start.
	result, err := s.db.ExecContext(ctx, `
		UPDATE incidents
		SET resolved_at_ms = ?
		WHERE service_id = ?
		  AND started_at_ms = ?
		  AND resolved_at_ms IS NULL
	`, value.ResolvedAt.UnixMilli(), value.ServiceID, value.StartedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("resolve incident for service %q: %w", value.ServiceID, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve incident for service %q: count updated rows: %w", value.ServiceID, err)
	}
	if count == 0 {
		return fmt.Errorf("resolve incident for service %q: no matching unresolved incident started at %d", value.ServiceID, value.StartedAt.UnixMilli())
	}
	if count != 1 {
		return fmt.Errorf("resolve incident for service %q: updated %d unresolved incidents, want 1", value.ServiceID, count)
	}
	return nil
}
