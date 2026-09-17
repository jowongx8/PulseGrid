package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/pressly/goose/v3"
)

func TestCheckResultStoreSavesRawResults(t *testing.T) {
	checkedAt := time.Unix(1_700_000_000, 123_456_789).In(time.FixedZone("AEST", 10*60*60))
	duration := time.Second + 234*time.Millisecond + 567*time.Microsecond
	tests := []struct {
		name             string
		statusCode       int
		errorKind        monitoring.ErrorKind
		errorMessage     string
		wantStatus       sql.NullInt64
		wantErrorMessage sql.NullString
	}{
		{
			name:       "HTTP 200",
			statusCode: 200,
			wantStatus: sql.NullInt64{Int64: 200, Valid: true},
		},
		{
			name:       "HTTP 503 remains raw",
			statusCode: 503,
			wantStatus: sql.NullInt64{Int64: 503, Valid: true},
		},
		{
			name:             "DNS failure has no response",
			errorKind:        monitoring.ErrorDNS,
			errorMessage:     "lookup failed",
			wantErrorMessage: sql.NullString{String: "lookup failed", Valid: true},
		},
		{
			name:      "canceled result can be stored",
			errorKind: monitoring.ErrorCanceled,
		},
		{
			name:       "nonstandard status is preserved",
			statusCode: -7,
			errorKind:  monitoring.ErrorKind("unexpected"),
			wantStatus: sql.NullInt64{Int64: -7, Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openCheckResultTestDB(t)
			store := NewCheckResultStore(db)
			result := monitoring.CheckResult{
				ServiceID:    "example",
				CheckedAt:    checkedAt,
				Duration:     duration,
				StatusCode:   tt.statusCode,
				ErrorKind:    tt.errorKind,
				ErrorMessage: tt.errorMessage,
			}
			if err := store.Save(context.Background(), result); err != nil {
				t.Fatalf("Save() error = %v", err)
			}

			var serviceID, errorKind string
			var checkedAtMS, durationMS int64
			var statusCode sql.NullInt64
			var errorMessage sql.NullString
			err := db.QueryRow(`
				SELECT service_id, checked_at_ms, duration_ms, status_code, error_kind, error_message
				FROM check_results
			`).Scan(&serviceID, &checkedAtMS, &durationMS, &statusCode, &errorKind, &errorMessage)
			if err != nil {
				t.Fatal(err)
			}
			if serviceID != "example" || checkedAtMS != 1_700_000_000_123 || durationMS != 1234 {
				t.Errorf("stored identity/time/duration = (%q, %d, %d), want (example, 1700000000123, 1234)", serviceID, checkedAtMS, durationMS)
			}
			if statusCode != tt.wantStatus || errorKind != string(tt.errorKind) || errorMessage != tt.wantErrorMessage {
				t.Errorf("stored status/error = (%+v, %q, %+v), want (%+v, %q, %+v)", statusCode, errorKind, errorMessage, tt.wantStatus, tt.errorKind, tt.wantErrorMessage)
			}
		})
	}
}

func TestCheckResultStoreAcceptsUnixEpoch(t *testing.T) {
	db := openCheckResultTestDB(t)
	result := validCheckResult()
	result.CheckedAt = time.Unix(0, 0)
	if err := NewCheckResultStore(db).Save(context.Background(), result); err != nil {
		t.Fatalf("Save() at Unix epoch: %v", err)
	}
	var checkedAtMS int64
	if err := db.QueryRow("SELECT checked_at_ms FROM check_results").Scan(&checkedAtMS); err != nil {
		t.Fatal(err)
	}
	if checkedAtMS != 0 {
		t.Fatalf("checked_at_ms = %d, want 0", checkedAtMS)
	}
}

func TestCheckResultStoreAllowsDuplicateServiceAndTimestamp(t *testing.T) {
	db := openCheckResultTestDB(t)
	store := NewCheckResultStore(db)
	first := validCheckResult()
	first.StatusCode = 200
	second := first
	second.StatusCode = 503
	for _, result := range []monitoring.CheckResult{first, second} {
		if err := store.Save(context.Background(), result); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query("SELECT id, service_id, checked_at_ms, status_code FROM check_results ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	var statuses []int
	for rows.Next() {
		var id, checkedAtMS int64
		var serviceID string
		var statusCode int
		if err := rows.Scan(&id, &serviceID, &checkedAtMS, &statusCode); err != nil {
			t.Fatal(err)
		}
		if serviceID != first.ServiceID || checkedAtMS != first.CheckedAt.UnixMilli() {
			t.Fatalf("stored service/time = (%q, %d), want (%q, %d)", serviceID, checkedAtMS, first.ServiceID, first.CheckedAt.UnixMilli())
		}
		ids = append(ids, id)
		statuses = append(statuses, statusCode)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] == ids[1] || len(statuses) != 2 || statuses[0] != 200 || statuses[1] != 503 {
		t.Fatalf("rows = ids %v, statuses %v; want distinct IDs and statuses [200 503]", ids, statuses)
	}
}

func TestCheckResultStoreRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		change func(*monitoring.CheckResult)
		want   string
	}{
		{"empty service ID", func(r *monitoring.CheckResult) { r.ServiceID = "" }, "service ID"},
		{"zero check time", func(r *monitoring.CheckResult) { r.CheckedAt = time.Time{} }, "check time"},
		{"negative duration", func(r *monitoring.CheckResult) { r.Duration = -time.Nanosecond }, "duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openCheckResultTestDB(t)
			result := validCheckResult()
			tt.change(&result)
			if err := NewCheckResultStore(db).Save(context.Background(), result); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Save() error = %v, want %q", err, tt.want)
			}
			assertCheckResultCount(t, db, 0)
		})
	}
}

func TestCheckResultsSchemaRejectsInvalidRows(t *testing.T) {
	tests := []struct {
		name       string
		serviceID  string
		durationMS int64
	}{
		{"empty service ID", "", 1},
		{"negative duration", "example", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openCheckResultTestDB(t)
			_, err := db.Exec(`
				INSERT INTO check_results (service_id, checked_at_ms, duration_ms, error_kind)
				VALUES (?, ?, ?, ?)
			`, tt.serviceID, 0, tt.durationMS, "")
			if err == nil {
				t.Fatal("direct INSERT succeeded, want schema constraint error")
			}
			assertCheckResultCount(t, db, 0)
		})
	}
}

func TestCheckResultStoreRespectsCanceledContext(t *testing.T) {
	db := openCheckResultTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewCheckResultStore(db).Save(ctx, validCheckResult()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save() error = %v, want context.Canceled", err)
	}
	assertCheckResultCount(t, db, 0)
}

func TestCheckResultsDownMigrationPreservesBaseline(t *testing.T) {
	db := openCheckResultTestDB(t)
	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(context.Background()); err != nil {
		t.Fatal(err)
	}

	var tableCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'check_results'").Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 {
		t.Fatalf("check_results table count after Down = %d, want 0", tableCount)
	}
	var latest int
	if err := db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&latest); err != nil {
		t.Fatal(err)
	}
	if latest != 1 {
		t.Fatalf("latest migration version after Down = %d, want baseline version 1", latest)
	}
}

func openCheckResultTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "pulsegrid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db
}

func validCheckResult() monitoring.CheckResult {
	return monitoring.CheckResult{
		ServiceID:  "example",
		CheckedAt:  time.Unix(1_700_000_000, 0),
		Duration:   time.Second,
		StatusCode: 200,
		ErrorKind:  monitoring.ErrorNone,
	}
}

func assertCheckResultCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM check_results").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("check_results count = %d, want %d", got, want)
	}
}
