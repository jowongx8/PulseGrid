package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jowongx8/backend/internal/incident"
)

// IncidentStore persists and loads incidents.
type IncidentStore struct {
	db *sql.DB
}

func NewIncidentStore(db *sql.DB) *IncidentStore {
	return &IncidentStore{db: db}
}

// ListFeed reads all active incidents and fills the target with recent resolved
// incidents from one database snapshot.
func (s *IncidentStore) ListFeed(ctx context.Context, target int) (active []incident.Incident, resolved []incident.Incident, retErr error) {
	if target < 0 {
		return nil, nil, errors.New("list incident feed: target must not be negative")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("begin incident feed read: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			active = nil
			resolved = nil
			retErr = errors.Join(retErr, fmt.Errorf("rollback incident feed read: %w", err))
		}
	}()

	active, err = listActiveIncidents(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	resolved = make([]incident.Incident, 0)
	remaining := target - len(active)
	if remaining > 0 {
		resolved, err = listResolvedIncidents(ctx, tx, remaining)
		if err != nil {
			return nil, nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit incident feed read: %w", err)
	}
	return active, resolved, nil
}

func listActiveIncidents(ctx context.Context, tx *sql.Tx) ([]incident.Incident, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT service_id, started_at_ms
		FROM incidents
		WHERE resolved_at_ms IS NULL
		ORDER BY started_at_ms DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query active incident feed: %w", err)
	}
	defer rows.Close()

	values := make([]incident.Incident, 0)
	for rows.Next() {
		var serviceID string
		var startedAtMS int64
		if err := rows.Scan(&serviceID, &startedAtMS); err != nil {
			return nil, fmt.Errorf("scan active incident feed: %w", err)
		}
		values = append(values, incident.Incident{
			ServiceID: serviceID,
			StartedAt: time.UnixMilli(startedAtMS),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active incident feed: %w", err)
	}
	return values, nil
}

func listResolvedIncidents(ctx context.Context, tx *sql.Tx, limit int) ([]incident.Incident, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT service_id, started_at_ms, resolved_at_ms
		FROM incidents
		WHERE resolved_at_ms IS NOT NULL
		ORDER BY resolved_at_ms DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query resolved incident feed: %w", err)
	}
	defer rows.Close()

	values := make([]incident.Incident, 0, limit)
	for rows.Next() {
		var serviceID string
		var startedAtMS, resolvedAtMS int64
		if err := rows.Scan(&serviceID, &startedAtMS, &resolvedAtMS); err != nil {
			return nil, fmt.Errorf("scan resolved incident feed: %w", err)
		}
		resolvedAt := time.UnixMilli(resolvedAtMS)
		values = append(values, incident.Incident{
			ServiceID:  serviceID,
			StartedAt:  time.UnixMilli(startedAtMS),
			ResolvedAt: &resolvedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resolved incident feed: %w", err)
	}
	return values, nil
}

func (s *IncidentStore) ListOpen(ctx context.Context) ([]incident.Incident, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT service_id, started_at_ms
		FROM incidents
		WHERE resolved_at_ms IS NULL
		ORDER BY service_id
	`)
	if err != nil {
		return nil, fmt.Errorf("query open incidents: %w", err)
	}
	defer rows.Close()

	values := make([]incident.Incident, 0)
	for rows.Next() {
		var serviceID string
		var startedAtMS int64
		if err := rows.Scan(&serviceID, &startedAtMS); err != nil {
			return nil, fmt.Errorf("scan open incident: %w", err)
		}
		values = append(values, incident.Incident{
			ServiceID: serviceID,
			StartedAt: time.UnixMilli(startedAtMS),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open incidents: %w", err)
	}
	return values, nil
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
