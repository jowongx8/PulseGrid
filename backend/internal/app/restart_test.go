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
	assertPersistedIncident(t, db, startedAt.UnixMicro(), sql.NullInt64{})
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
	assertPersistedIncident(t, db, startedAt.UnixMicro(), sql.NullInt64{Int64: resolvedAt.UnixMicro(), Valid: true})
	if active, ok := lifecycle.Active("github"); ok {
		t.Fatalf("resolved incident remains active: %+v", active)
	}
}

func TestIncidentProcessorCanonicalIdentityWithoutRestart(t *testing.T) {
	_, store := openRestartTestStore(t)
	ctx := context.Background()
	lifecycle := incident.NewLifecycle()
	processor := NewIncidentProcessor(lifecycle, store)
	startedAt := time.Date(2026, 9, 19, 10, 0, 0, 123_456_789, time.FixedZone("AEST", 10*60*60))
	wantStartedAt := time.UnixMicro(startedAt.UnixMicro()).UTC()

	opened, err := processor.Process(ctx, testIncidentUpdate(status.StatusUp, status.StatusDown, startedAt))
	if err != nil {
		t.Fatal(err)
	}
	if opened == nil || opened.StartedAt != wantStartedAt {
		t.Fatalf("opened transition = %+v, want start %s", opened, wantStartedAt)
	}
	active, ok := lifecycle.Active("example")
	if !ok || active.StartedAt != startedAt {
		t.Fatalf("active lifecycle incident = (%+v, %t), want original start %s", active, ok, startedAt)
	}

	resolvedAt := startedAt.Add(time.Minute + 987*time.Nanosecond)
	wantResolvedAt := time.UnixMicro(resolvedAt.UnixMicro()).UTC()
	resolved, err := processor.Process(ctx, testIncidentUpdate(status.StatusDown, status.StatusUp, resolvedAt))
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.StartedAt != wantStartedAt || resolved.ResolvedAt == nil || *resolved.ResolvedAt != wantResolvedAt {
		t.Fatalf("resolved transition = %+v, want start %s and resolution %s", resolved, wantStartedAt, wantResolvedAt)
	}

	activeFeed, resolvedFeed, err := store.ListFeed(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeFeed) != 0 || len(resolvedFeed) != 1 || resolvedFeed[0].StartedAt != wantStartedAt ||
		resolvedFeed[0].ResolvedAt == nil || *resolvedFeed[0].ResolvedAt != wantResolvedAt {
		t.Fatalf("incident feed = (%+v active, %+v resolved), want one canonical resolution", activeFeed, resolvedFeed)
	}
}

func TestIncidentCanonicalIdentitySurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pulsegrid.db")
	db, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewIncidentStore(db)
	processor := NewIncidentProcessor(incident.NewLifecycle(), store)
	startedAt := time.Date(2026, 9, 19, 10, 0, 0, 123_456_789, time.FixedZone("AEST", 10*60*60))
	wantStartedAt := time.UnixMicro(startedAt.UnixMicro()).UTC()

	opened, err := processor.Process(ctx, testIncidentUpdate(status.StatusUp, status.StatusDown, startedAt))
	if err != nil {
		t.Fatal(err)
	}
	if opened == nil || opened.StartedAt != wantStartedAt {
		t.Fatalf("opened transition = %+v, want start %s", opened, wantStartedAt)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close restarted database: %v", err)
		}
	})
	store = sqlite.NewIncidentStore(db)
	restoredLifecycle := incident.NewLifecycle()
	if err := RestoreOpenIncidents(ctx, restoredLifecycle, store); err != nil {
		t.Fatal(err)
	}
	restored, ok := restoredLifecycle.Active("example")
	if !ok || restored.StartedAt != wantStartedAt || restored.StartedAt.Location() != time.UTC {
		t.Fatalf("restored incident = (%+v, %t), want UTC start %s", restored, ok, wantStartedAt)
	}

	resolvedAt := startedAt.Add(time.Minute + 987*time.Nanosecond)
	wantResolvedAt := time.UnixMicro(resolvedAt.UnixMicro()).UTC()
	resolved, err := NewIncidentProcessor(restoredLifecycle, store).Process(
		ctx,
		testIncidentUpdate(status.StatusDown, status.StatusUp, resolvedAt),
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.StartedAt != opened.StartedAt || resolved.ResolvedAt == nil || *resolved.ResolvedAt != wantResolvedAt {
		t.Fatalf("resolved transition = %+v, want opening start %s and resolution %s", resolved, opened.StartedAt, wantResolvedAt)
	}

	query, err := NewIncidentsQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := query.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Active) != 0 || len(view.Resolved) != 1 || view.Resolved[0].StartedAt != opened.StartedAt ||
		view.Resolved[0].ResolvedAt == nil || *view.Resolved[0].ResolvedAt != wantResolvedAt ||
		view.Resolved[0].StartedAt.Location() != time.UTC || view.Resolved[0].ResolvedAt.Location() != time.UTC {
		t.Fatalf("REST incident view = %+v, want one canonical UTC resolution", view)
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
	if _, err := processor.Process(ctx, update); err != nil {
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
	if err := db.QueryRow("SELECT started_at_us, resolved_at_us FROM incidents WHERE service_id = ?", "github").Scan(&start, &resolution); err != nil {
		t.Fatal(err)
	}
	if start != wantStart || resolution != wantResolution {
		t.Fatalf("persisted incident = (%d, %+v), want (%d, %+v)", start, resolution, wantStart, wantResolution)
	}
}
