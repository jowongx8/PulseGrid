package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jowongx8/backend/internal/monitoring"
)

// CheckResultStore persists completed checks as raw historical observations.
type CheckResultStore struct {
	db *sql.DB
}

func NewCheckResultStore(db *sql.DB) *CheckResultStore {
	return &CheckResultStore{db: db}
}

func (s *CheckResultStore) Save(ctx context.Context, result monitoring.CheckResult) error {
	if result.ServiceID == "" {
		return errors.New("save check result: service ID must not be empty")
	}
	if result.CheckedAt.IsZero() {
		return errors.New("save check result: check time must not be zero")
	}
	if result.Duration < 0 {
		return errors.New("save check result: duration must not be negative")
	}

	statusCode := sql.NullInt64{}
	if result.StatusCode != 0 {
		statusCode = sql.NullInt64{Int64: int64(result.StatusCode), Valid: true}
	}
	errorMessage := sql.NullString{}
	if result.ErrorMessage != "" {
		errorMessage = sql.NullString{String: result.ErrorMessage, Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO check_results (
			service_id, checked_at_ms, duration_ms, status_code, error_kind, error_message
		) VALUES (?, ?, ?, ?, ?, ?)
	`, result.ServiceID, result.CheckedAt.UnixMilli(), result.Duration.Milliseconds(), statusCode, string(result.ErrorKind), errorMessage)
	if err != nil {
		return fmt.Errorf("save check result for service %q: %w", result.ServiceID, err)
	}
	return nil
}
