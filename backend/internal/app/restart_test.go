package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/status"
	"github.com/jowongx8/backend/internal/storage/sqlite"
)

func TestRestartReconciliationKeepsExistingIncidentWhenServiceIsStillDown(t *testing.T) {
	db, store := openRestartTestStore(t)
	ctx := context.Background()
	startedAt := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	if err := store.Open(ctx, incident.Incident{ServiceID: "github", StartedAt: startedAt}); err != nil {
		t.Fatal(err)
	}

	lifecycle := incident.NewLifecycle()
	if err := RestoreOpenIncidents(ctx, lifecycle, store); err != nil {
		t.Fatalf("RestoreOpenIncidents() error = %v", err)
	}
	tracker := newRestartTracker(t)
	assertRestartStatus(t, tracker, status.StatusUnknown)
	processor := NewIncidentProcessor(lifecycle, store)

	for index := 1; index <= 3; index++ {
		applyRestartResult(t, ctx, tracker, processor, monitoring.CheckResult{
			ServiceID:  "github",
			CheckedAt:  startedAt.Add(time.Duration(index) * time.Second),
			StatusCode: 503,
		})
	}

	assertRestartStatus(t, tracker, status.StatusDown)
	assertPersistedIncident(t, db, startedAt.UnixMilli(), sql.NullInt64{})
	active, ok := lifecycle.Active("github")
	if !ok || !active.StartedAt.Equal(startedAt) || active.ResolvedAt != nil {
		t.Fatalf("Active(github) = (%+v, %t), want original active incident", active, ok)
	}
}

func TestRestartReconciliationResolvesIncidentWhenServiceRecoveredOffline(t *testing.T) {
	db, store := openRestartTestStore(t)
	ctx := context.Background()
	startedAt := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	if err := store.Open(ctx, incident.Incident{ServiceID: "github", StartedAt: startedAt}); err != nil {
		t.Fatal(err)
	}

	lifecycle := incident.NewLifecycle()
	if err := RestoreOpenIncidents(ctx, lifecycle, store); err != nil {
		t.Fatalf("RestoreOpenIncidents() error = %v", err)
	}
	tracker := newRestartTracker(t)
	assertRestartStatus(t, tracker, status.StatusUnknown)
	processor := NewIncidentProcessor(lifecycle, store)
	resolvedAt := startedAt.Add(time.Minute)

	applyRestartResult(t, ctx, tracker, processor, monitoring.CheckResult{
		ServiceID:  "github",
		CheckedAt:  resolvedAt,
		StatusCode: 204,
	})

	assertRestartStatus(t, tracker, status.StatusUp)
	assertPersistedIncident(t, db, startedAt.UnixMilli(), sql.NullInt64{Int64: resolvedAt.UnixMilli(), Valid: true})
	if active, ok := lifecycle.Active("github"); ok {
		t.Fatalf("resolved incident remains active: %+v", active)
	}
}

func openRestartTestStore(t *testing.T) (*sql.DB, *sqlite.IncidentStore) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pulsegrid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close restart test database: %v", err)
		}
	})
	return db, sqlite.NewIncidentStore(db)
}

func newRestartTracker(t *testing.T) *status.Tracker {
	t.Helper()
	tracker, err := status.NewTracker([]string{"github"})
	if err != nil {
		t.Fatal(err)
	}
	return tracker
}

func applyRestartResult(
	t *testing.T,
	ctx context.Context,
	tracker *status.Tracker,
	processor *IncidentProcessor,
	result monitoring.CheckResult,
) {
	t.Helper()
	update, err := tracker.Apply(monitoring.Evaluate(result))
	if err != nil {
		t.Fatalf("Tracker.Apply() error = %v", err)
	}
	if err := processor.Process(ctx, update); err != nil {
		t.Fatalf("IncidentProcessor.Process() error = %v", err)
	}
}

func assertRestartStatus(t *testing.T, tracker *status.Tracker, want status.ServiceStatus) {
	t.Helper()
	snapshot, err := tracker.Get("github")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != want {
		t.Fatalf("github status = %q, want %q", snapshot.Status, want)
	}
}

func assertPersistedIncident(t *testing.T, db *sql.DB, wantStart int64, wantResolution sql.NullInt64) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM incidents WHERE service_id = ?", "github").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("github incident row count = %d, want 1", count)
	}

	var start int64
	var resolution sql.NullInt64
	if err := db.QueryRow("SELECT started_at_ms, resolved_at_ms FROM incidents WHERE service_id = ?", "github").Scan(&start, &resolution); err != nil {
		t.Fatal(err)
	}
	if start != wantStart || resolution != wantResolution {
		t.Fatalf("persisted incident = (%d, %+v), want (%d, %+v)", start, resolution, wantStart, wantResolution)
	}
}
