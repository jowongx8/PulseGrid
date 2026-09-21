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

	"github.com/jowongx8/backend/internal/incident"
	"github.com/pressly/goose/v3"
)

func TestIncidentStoreListOpenReturnsEmptyResult(t *testing.T) {
	values, err := NewIncidentStore(openIncidentTestDB(t)).ListOpen(context.Background())
	if err != nil {
		t.Fatalf("ListOpen() error = %v", err)
	}
	if values == nil || len(values) != 0 {
		t.Fatalf("ListOpen() = %#v, want non-nil empty slice", values)
	}
}

func TestIncidentStoreListOpenRestoresStoredMilliseconds(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	original := incident.Incident{
		ServiceID: "github",
		StartedAt: time.Unix(1_700_000_000, 123_456_789).In(time.FixedZone("AEST", 10*60*60)),
	}
	if err := store.Open(context.Background(), original); err != nil {
		t.Fatal(err)
	}

	values, err := store.ListOpen(context.Background())
	if err != nil {
		t.Fatalf("ListOpen() error = %v", err)
	}
	if len(values) != 1 || values[0].ServiceID != original.ServiceID ||
		!values[0].StartedAt.Equal(time.UnixMilli(original.StartedAt.UnixMilli())) || values[0].ResolvedAt != nil {
		t.Fatalf("ListOpen() = %+v, want %q at stored millisecond %s", values, original.ServiceID, time.UnixMilli(original.StartedAt.UnixMilli()))
	}
}

func TestIncidentStoreListOpenOrdersServicesAndExcludesResolvedHistory(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	resolved := incident.Incident{ServiceID: "cloudflare", StartedAt: time.UnixMilli(1000)}
	if err := store.Open(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
	resolvedAt := resolved.StartedAt.Add(time.Second)
	resolved.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}

	for _, value := range []incident.Incident{
		{ServiceID: "openai", StartedAt: time.UnixMilli(3000)},
		{ServiceID: "github", StartedAt: time.UnixMilli(2000)},
	} {
		if err := store.Open(context.Background(), value); err != nil {
			t.Fatal(err)
		}
	}

	values, err := store.ListOpen(context.Background())
	if err != nil {
		t.Fatalf("ListOpen() error = %v", err)
	}
	if len(values) != 2 || values[0].ServiceID != "github" || values[1].ServiceID != "openai" {
		t.Fatalf("ListOpen() = %+v, want github then openai", values)
	}
	for _, value := range values {
		if value.ResolvedAt != nil {
			t.Fatalf("ListOpen() returned resolved incident %+v", value)
		}
	}
}

func TestIncidentStoreListOpenRespectsCanceledContext(t *testing.T) {
	db := openIncidentTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewIncidentStore(db).ListOpen(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListOpen() error = %v, want context.Canceled", err)
	}
}

func TestIncidentStoreListOpenReportsQueryFailure(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ListOpen(context.Background()); err == nil || !strings.Contains(err.Error(), "query open incidents") {
		t.Fatalf("ListOpen() error = %v, want contextual query failure", err)
	}
}

func TestIncidentStoreListOpenReportsScanFailure(t *testing.T) {
	db := openIncidentTestDB(t)
	if _, err := db.Exec(`
		INSERT INTO incidents (service_id, started_at_ms, resolved_at_ms)
		VALUES (?, ?, NULL)
	`, "github", "not-a-timestamp"); err != nil {
		t.Fatalf("insert malformed incident: %v", err)
	}

	if _, err := NewIncidentStore(db).ListOpen(context.Background()); err == nil || !strings.Contains(err.Error(), "scan open incident") {
		t.Fatalf("ListOpen() error = %v, want contextual scan failure", err)
	}
}

func TestIncidentStoreOpenAndResolve(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	start := time.Unix(1_700_000_000, 123_456_789).In(time.FixedZone("AEST", 10*60*60))
	opened := incident.Incident{ServiceID: "github", StartedAt: start}
	if err := store.Open(context.Background(), opened); err != nil {
		t.Fatal(err)
	}

	var id int64
	var serviceID string
	var startedAtMS int64
	var resolvedAtMS sql.NullInt64
	if err := db.QueryRow("SELECT id, service_id, started_at_ms, resolved_at_ms FROM incidents").Scan(&id, &serviceID, &startedAtMS, &resolvedAtMS); err != nil {
		t.Fatal(err)
	}
	if id <= 0 || serviceID != "github" || startedAtMS != start.UnixMilli() || resolvedAtMS.Valid {
		t.Fatalf("opened row = (%d, %q, %d, %+v), want positive ID, github, %d, NULL", id, serviceID, startedAtMS, resolvedAtMS, start.UnixMilli())
	}
	assertIncidentCount(t, db, 1)

	resolvedAt := start.Add(45*time.Second + 500*time.Microsecond)
	resolved := opened
	resolved.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
	var resolvedID int64
	if err := db.QueryRow("SELECT id, resolved_at_ms FROM incidents").Scan(&resolvedID, &resolvedAtMS); err != nil {
		t.Fatal(err)
	}
	if resolvedID != id || !resolvedAtMS.Valid || resolvedAtMS.Int64 != resolvedAt.UnixMilli() {
		t.Fatalf("resolved row = (%d, %+v), want (%d, %d)", resolvedID, resolvedAtMS, id, resolvedAt.UnixMilli())
	}
	assertIncidentCount(t, db, 1)
}

func TestIncidentStoreKeepsResolvedHistory(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	first := validIncident()
	if err := store.Open(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	resolvedAt := first.StartedAt.Add(5 * time.Minute)
	first.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := incident.Incident{ServiceID: first.ServiceID, StartedAt: first.StartedAt.Add(10 * time.Minute)}
	if err := store.Open(context.Background(), second); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("SELECT started_at_ms, resolved_at_ms FROM incidents WHERE service_id = ? ORDER BY id", first.ServiceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var starts []int64
	var resolutions []sql.NullInt64
	for rows.Next() {
		var start int64
		var resolution sql.NullInt64
		if err := rows.Scan(&start, &resolution); err != nil {
			t.Fatal(err)
		}
		starts = append(starts, start)
		resolutions = append(resolutions, resolution)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(starts) != 2 || starts[0] != first.StartedAt.UnixMilli() || starts[1] != second.StartedAt.UnixMilli() ||
		!resolutions[0].Valid || resolutions[0].Int64 != resolvedAt.UnixMilli() || resolutions[1].Valid {
		t.Fatalf("history = starts %v, resolutions %v; want resolved first incident and open second incident", starts, resolutions)
	}
}

func TestIncidentStoreRejectsDuplicateOpen(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	first := validIncident()
	if err := store.Open(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.StartedAt = second.StartedAt.Add(time.Hour)
	if err := store.Open(context.Background(), second); err == nil {
		t.Fatal("second Open() succeeded, want uniqueness error")
	}
	assertIncidentCount(t, db, 1)
	var start int64
	var resolution sql.NullInt64
	if err := db.QueryRow("SELECT started_at_ms, resolved_at_ms FROM incidents").Scan(&start, &resolution); err != nil {
		t.Fatal(err)
	}
	if start != first.StartedAt.UnixMilli() || resolution.Valid {
		t.Fatalf("remaining incident = (%d, %+v), want original unresolved incident", start, resolution)
	}
}

func TestIncidentStoreAllowsDifferentServicesToBeOpen(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	for _, serviceID := range []string{"github", "openai"} {
		value := validIncident()
		value.ServiceID = serviceID
		if err := store.Open(context.Background(), value); err != nil {
			t.Fatalf("Open(%q): %v", serviceID, err)
		}
	}
	var openCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM incidents WHERE resolved_at_ms IS NULL").Scan(&openCount); err != nil {
		t.Fatal(err)
	}
	if openCount != 2 {
		t.Fatalf("open incident count = %d, want 2", openCount)
	}
}

func TestIncidentStoreResolveMatchesExactOpenIncident(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	first := validIncident()
	if err := store.Open(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	firstResolvedAt := first.StartedAt.Add(time.Minute)
	first.ResolvedAt = &firstResolvedAt
	if err := store.Resolve(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := incident.Incident{ServiceID: first.ServiceID, StartedAt: first.StartedAt.Add(2 * time.Minute)}
	if err := store.Open(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	other := incident.Incident{ServiceID: "openai", StartedAt: second.StartedAt}
	if err := store.Open(context.Background(), other); err != nil {
		t.Fatal(err)
	}

	resolvedAt := second.StartedAt.Add(time.Minute)
	for _, mismatch := range []incident.Incident{
		{ServiceID: second.ServiceID, StartedAt: second.StartedAt.Add(time.Second), ResolvedAt: &resolvedAt},
		{ServiceID: "missing-service", StartedAt: second.StartedAt, ResolvedAt: &resolvedAt},
	} {
		if err := store.Resolve(context.Background(), mismatch); err == nil || !strings.Contains(err.Error(), "no matching unresolved incident") {
			t.Fatalf("Resolve(%+v) error = %v, want missing-row error", mismatch, err)
		}
	}
	var unresolved int
	if err := db.QueryRow("SELECT COUNT(*) FROM incidents WHERE resolved_at_ms IS NULL").Scan(&unresolved); err != nil {
		t.Fatal(err)
	}
	if unresolved != 2 {
		t.Fatalf("unresolved count after mismatched Resolve = %d, want 2", unresolved)
	}

	second.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	var firstResolution, secondResolution, otherResolution sql.NullInt64
	for _, tc := range []struct {
		value incident.Incident
		got   *sql.NullInt64
	}{
		{first, &firstResolution},
		{second, &secondResolution},
		{other, &otherResolution},
	} {
		if err := db.QueryRow("SELECT resolved_at_ms FROM incidents WHERE service_id = ? AND started_at_ms = ?", tc.value.ServiceID, tc.value.StartedAt.UnixMilli()).Scan(tc.got); err != nil {
			t.Fatal(err)
		}
	}
	if !firstResolution.Valid || firstResolution.Int64 != firstResolvedAt.UnixMilli() ||
		!secondResolution.Valid || secondResolution.Int64 != resolvedAt.UnixMilli() || otherResolution.Valid {
		t.Fatalf("resolutions = (%+v, %+v, %+v), want first and second resolved, other open", firstResolution, secondResolution, otherResolution)
	}
}

func TestIncidentStoreResolveMissingOrAlreadyResolvedReturnsError(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	value := validIncident()
	resolvedAt := value.StartedAt.Add(time.Minute)
	value.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), value); err == nil {
		t.Fatal("Resolve() without an open row succeeded")
	}
	assertIncidentCount(t, db, 0)

	opened := value
	opened.ResolvedAt = nil
	if err := store.Open(context.Background(), opened); err != nil {
		t.Fatal(err)
	}
	if err := store.Resolve(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if err := store.Resolve(context.Background(), value); err == nil {
		t.Fatal("Resolve() on an already resolved row succeeded")
	}
	assertIncidentCount(t, db, 1)
}

func TestIncidentStoreOpenRejectsInvalidValues(t *testing.T) {
	resolvedAt := validIncident().StartedAt.Add(time.Minute)
	tests := []struct {
		name   string
		change func(*incident.Incident)
		want   string
	}{
		{"empty service ID", func(v *incident.Incident) { v.ServiceID = "" }, "service ID"},
		{"zero start time", func(v *incident.Incident) { v.StartedAt = time.Time{} }, "start time"},
		{"already resolved", func(v *incident.Incident) { v.ResolvedAt = &resolvedAt }, "resolution time"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openIncidentTestDB(t)
			value := validIncident()
			tc.change(&value)
			if err := NewIncidentStore(db).Open(context.Background(), value); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Open() error = %v, want %q", err, tc.want)
			}
			assertIncidentCount(t, db, 0)
		})
	}
}

func TestIncidentStoreResolveRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		change func(*incident.Incident)
		want   string
	}{
		{"empty service ID", func(v *incident.Incident) { v.ServiceID = "" }, "service ID"},
		{"zero start time", func(v *incident.Incident) { v.StartedAt = time.Time{} }, "start time"},
		{"missing resolution time", func(v *incident.Incident) { v.ResolvedAt = nil }, "resolution time"},
		{"resolution before start", func(v *incident.Incident) {
			before := v.StartedAt.Add(-time.Second)
			v.ResolvedAt = &before
		}, "after start time"},
		{"resolution equals start", func(v *incident.Incident) {
			equal := v.StartedAt
			v.ResolvedAt = &equal
		}, "after start time"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openIncidentTestDB(t)
			store := NewIncidentStore(db)
			opened := validIncident()
			if err := store.Open(context.Background(), opened); err != nil {
				t.Fatal(err)
			}
			resolvedAt := opened.StartedAt.Add(time.Minute)
			value := opened
			value.ResolvedAt = &resolvedAt
			tc.change(&value)
			if err := store.Resolve(context.Background(), value); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Resolve() error = %v, want %q", err, tc.want)
			}
			var resolution sql.NullInt64
			if err := db.QueryRow("SELECT resolved_at_ms FROM incidents").Scan(&resolution); err != nil {
				t.Fatal(err)
			}
			if resolution.Valid {
				t.Fatalf("invalid Resolve() changed row to %+v, want NULL", resolution)
			}
			assertIncidentCount(t, db, 1)
		})
	}
}

func TestIncidentStoreAcceptsUnixEpoch(t *testing.T) {
	db := openIncidentTestDB(t)
	value := validIncident()
	value.StartedAt = time.Unix(0, 0)
	if err := NewIncidentStore(db).Open(context.Background(), value); err != nil {
		t.Fatalf("Open() at Unix epoch: %v", err)
	}
	var storedStart int64
	if err := db.QueryRow("SELECT started_at_ms FROM incidents").Scan(&storedStart); err != nil {
		t.Fatal(err)
	}
	if storedStart != 0 {
		t.Fatalf("started_at_ms = %d, want 0", storedStart)
	}
}

func TestIncidentSchemaRejectsInvalidResolutionOrder(t *testing.T) {
	for _, resolvedAtMS := range []int64{999, 1000} {
		t.Run(time.UnixMilli(resolvedAtMS).Format(time.RFC3339Nano), func(t *testing.T) {
			db := openIncidentTestDB(t)
			if _, err := db.Exec("INSERT INTO incidents (service_id, started_at_ms, resolved_at_ms) VALUES (?, ?, ?)", "github", 1000, resolvedAtMS); err == nil {
				t.Fatal("direct INSERT succeeded, want CHECK constraint error")
			}
			assertIncidentCount(t, db, 0)
		})
	}
}

func TestIncidentStoreSurfacesMillisecondPrecisionCollision(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	base := time.Unix(1_700_000_000, 0)
	value := incident.Incident{ServiceID: "github", StartedAt: base.Add(100 * time.Microsecond)}
	if err := store.Open(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	resolvedAt := base.Add(900 * time.Microsecond)
	if !resolvedAt.After(value.StartedAt) || resolvedAt.UnixMilli() != value.StartedAt.UnixMilli() {
		t.Fatal("test timestamps must be ordered in Go but equal in stored milliseconds")
	}
	value.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), value); err == nil {
		t.Fatal("Resolve() succeeded despite equal stored milliseconds")
	}
	var resolution sql.NullInt64
	if err := db.QueryRow("SELECT resolved_at_ms FROM incidents").Scan(&resolution); err != nil {
		t.Fatal(err)
	}
	if resolution.Valid {
		t.Fatalf("resolution after failed Resolve() = %+v, want NULL", resolution)
	}
}

func TestIncidentStoreRespectsCanceledContext(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value := validIncident()
	if err := store.Open(ctx, value); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() error = %v, want context.Canceled", err)
	}
	assertIncidentCount(t, db, 0)
	if err := store.Open(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	resolvedAt := value.StartedAt.Add(time.Minute)
	value.ResolvedAt = &resolvedAt
	if err := store.Resolve(ctx, value); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve() error = %v, want context.Canceled", err)
	}
	var resolution sql.NullInt64
	if err := db.QueryRow("SELECT resolved_at_ms FROM incidents").Scan(&resolution); err != nil {
		t.Fatal(err)
	}
	if resolution.Valid {
		t.Fatalf("resolution after canceled Resolve() = %+v, want NULL", resolution)
	}
}

func TestIncidentsDownMigrationPreservesCheckResults(t *testing.T) {
	db := openIncidentTestDB(t)
	if _, err := db.Exec("INSERT INTO check_results (service_id, checked_at_ms, duration_ms, error_kind) VALUES (?, ?, ?, ?)", "github", 1000, 5, ""); err != nil {
		t.Fatal(err)
	}
	var indexCount int
	var indexName sql.NullString
	if err := db.QueryRow("SELECT COUNT(*), MIN(name) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'incidents'").Scan(&indexCount, &indexName); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 || !indexName.Valid || indexName.String != "idx_incidents_one_open_per_service" {
		t.Fatalf("incident indexes = count %d, name %+v; want only idx_incidents_one_open_per_service", indexCount, indexName)
	}

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
	var incidentTables, incidentIndexes, checkResults, latest int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'incidents'").Scan(&incidentTables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'incidents'").Scan(&incidentIndexes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM check_results").Scan(&checkResults); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&latest); err != nil {
		t.Fatal(err)
	}
	if incidentTables != 0 || incidentIndexes != 0 || checkResults != 1 || latest != 2 {
		t.Fatalf("after Down: incident tables %d, indexes %d, check results %d, latest version %d; want 0, 0, 1, 2", incidentTables, incidentIndexes, checkResults, latest)
	}
}

func openIncidentTestDB(t *testing.T) *sql.DB {
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

func validIncident() incident.Incident {
	return incident.Incident{ServiceID: "github", StartedAt: time.Unix(1_700_000_000, 0)}
}

func assertIncidentCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM incidents").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("incident count = %d, want %d", got, want)
	}
}
