package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/incident"
	"github.com/pressly/goose/v3"
)

func TestIncidentStoreListFeedReturnsNonNilEmptyGroups(t *testing.T) {
	active, resolved, err := NewIncidentStore(openIncidentTestDB(t)).ListFeed(context.Background(), 20, []string{"github"})
	if err != nil {
		t.Fatalf("ListFeed() error = %v", err)
	}
	if active == nil || len(active) != 0 {
		t.Fatalf("active = %#v, want non-nil empty slice", active)
	}
	if resolved == nil || len(resolved) != 0 {
		t.Fatalf("resolved = %#v, want non-nil empty slice", resolved)
	}
}

func TestIncidentStoreFiltersMembershipBeforeFeedCapacity(t *testing.T) {
	db := openIncidentTestDB(t)
	for index := range 30 {
		insertStoredIncident(t, db, fmt.Sprintf("retired-active-%02d", index), int64(10_000+index), nil)
		insertStoredIncident(t, db, "retired-resolved", int64(20_000+index), int64Pointer(int64(30_000+index)))
	}
	insertStoredIncident(t, db, "github", 5_000, nil)
	insertStoredIncident(t, db, "openai", 6_000, nil)
	for index := range 25 {
		insertStoredIncident(t, db, "github", int64(100+index), int64Pointer(int64(200+index)))
	}
	store := NewIncidentStore(db)
	active, resolved, err := store.ListFeed(context.Background(), 20, []string{"github", "openai"})
	if err != nil {
		t.Fatal(err)
	}
	assertIncidentServiceOrder(t, active, []string{"openai", "github"})
	if len(resolved) != 18 || resolved[0].StartedAt.UnixMicro() != 124 || resolved[17].StartedAt.UnixMicro() != 107 {
		t.Fatalf("resolved = %+v, want 18 newest eligible incidents", resolved)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM incidents").Scan(&count); err != nil || count != 87 {
		t.Fatalf("historical row count = %d, error = %v; want all 87 rows retained", count, err)
	}
	open, err := store.ListOpen(context.Background())
	if err != nil || len(open) != 32 {
		t.Fatalf("restoration rows = %d, error = %v; want all 32 open histories", len(open), err)
	}
}

func TestIncidentStoreEmptyOrUnknownMembershipNeverExposesHistory(t *testing.T) {
	db := openIncidentTestDB(t)
	insertStoredIncident(t, db, "github", 100, nil)
	insertStoredIncident(t, db, "openai", 200, int64Pointer(300))
	for _, ids := range [][]string{nil, {}, {"missing"}, {"x') OR 1=1 --"}} {
		active, resolved, err := NewIncidentStore(db).ListFeed(context.Background(), 20, ids)
		if err != nil || active == nil || resolved == nil || len(active) != 0 || len(resolved) != 0 {
			t.Fatalf("membership %q returned (%+v, %+v, %v), want empty arrays", ids, active, resolved, err)
		}
	}
}

func TestIncidentSchemaUsesMicrosecondTimestampColumns(t *testing.T) {
	db := openIncidentTestDB(t)
	rows, err := db.Query("PRAGMA table_info(incidents)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var position, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&position, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"started_at_us", "resolved_at_us"} {
		if !columns[name] {
			t.Errorf("incidents column %q is missing", name)
		}
	}
	for _, name := range []string{"started_at_ms", "resolved_at_ms"} {
		if columns[name] {
			t.Errorf("legacy incidents column %q still exists", name)
		}
	}

	var indexSQL string
	if err := db.QueryRow(`
		SELECT sql
		FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_incidents_one_open_per_service'
	`).Scan(&indexSQL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(indexSQL, "WHERE resolved_at_us IS NULL") {
		t.Fatalf("open-incident index SQL = %q, want microsecond resolution predicate", indexSQL)
	}
}

func TestIncidentStoreListFeedOrdersGroupsDeterministically(t *testing.T) {
	db := openIncidentTestDB(t)
	for _, value := range []struct {
		serviceID string
		startedAt int64
		resolved  *int64
	}{
		{serviceID: "active-old", startedAt: 100},
		{serviceID: "active-tie-first", startedAt: 300},
		{serviceID: "active-tie-second", startedAt: 300},
		{serviceID: "resolved-old", startedAt: 400, resolved: int64Pointer(500)},
		{serviceID: "resolved-tie-first", startedAt: 600, resolved: int64Pointer(700)},
		{serviceID: "resolved-tie-second", startedAt: 650, resolved: int64Pointer(700)},
	} {
		insertStoredIncident(t, db, value.serviceID, value.startedAt, value.resolved)
	}

	active, resolved, err := NewIncidentStore(db).ListFeed(context.Background(), 10, []string{"active-old", "active-tie-first", "active-tie-second", "resolved-old", "resolved-tie-first", "resolved-tie-second"})
	if err != nil {
		t.Fatal(err)
	}
	assertIncidentServiceOrder(t, active, []string{"active-tie-second", "active-tie-first", "active-old"})
	assertIncidentServiceOrder(t, resolved, []string{"resolved-tie-second", "resolved-tie-first", "resolved-old"})
	for _, value := range active {
		if value.ResolvedAt != nil {
			t.Fatalf("active incident %+v has a resolution time", value)
		}
		if value.StartedAt.Location() != time.UTC {
			t.Fatalf("active incident location = %v, want UTC", value.StartedAt.Location())
		}
	}
	for _, value := range resolved {
		if value.ResolvedAt == nil {
			t.Fatalf("resolved incident %+v has no resolution time", value)
		}
		if value.StartedAt.Location() != time.UTC || value.ResolvedAt.Location() != time.UTC {
			t.Fatalf("resolved incident locations = (%v, %v), want UTC", value.StartedAt.Location(), value.ResolvedAt.Location())
		}
	}
}

func TestIncidentStoreListFeedFillsRemainingTargetCapacity(t *testing.T) {
	for _, activeCount := range []int{0, 3, 19, 20, 21, 25} {
		t.Run(fmt.Sprintf("%d active", activeCount), func(t *testing.T) {
			db := openIncidentTestDB(t)
			var serviceIDs []string
			for index := range 25 {
				serviceIDs = append(serviceIDs, fmt.Sprintf("resolved-%02d", index))
				insertStoredIncident(t, db, fmt.Sprintf("resolved-%02d", index), int64(1_000+index), int64Pointer(int64(10_000+index)))
			}
			for index := range activeCount {
				serviceIDs = append(serviceIDs, fmt.Sprintf("active-%02d", index))
				insertStoredIncident(t, db, fmt.Sprintf("active-%02d", index), int64(20_000+index), nil)
			}

			active, resolved, err := NewIncidentStore(db).ListFeed(context.Background(), 20, serviceIDs)
			if err != nil {
				t.Fatal(err)
			}
			wantResolved := 20 - activeCount
			if wantResolved < 0 {
				wantResolved = 0
			}
			if len(active) != activeCount || len(resolved) != wantResolved {
				t.Fatalf("ListFeed() counts = (%d active, %d resolved), want (%d, %d)", len(active), len(resolved), activeCount, wantResolved)
			}
			if len(resolved) > 0 && resolved[0].ServiceID != "resolved-24" {
				t.Fatalf("first resolved incident = %q, want newest resolved-24", resolved[0].ServiceID)
			}
			if len(resolved) > 0 {
				wantLast := fmt.Sprintf("resolved-%02d", 25-wantResolved)
				if got := resolved[len(resolved)-1].ServiceID; got != wantLast {
					t.Fatalf("last resolved incident = %q, want capacity boundary %q", got, wantLast)
				}
			}
		})
	}
}

func TestIncidentStoreListFeedTargetZeroKeepsAllActive(t *testing.T) {
	db := openIncidentTestDB(t)
	insertStoredIncident(t, db, "active", 2_000, nil)
	insertStoredIncident(t, db, "resolved", 1_000, int64Pointer(1_500))

	active, resolved, err := NewIncidentStore(db).ListFeed(context.Background(), 0, []string{"active", "resolved"})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ServiceID != "active" {
		t.Fatalf("active = %+v, want the active incident", active)
	}
	if resolved == nil || len(resolved) != 0 {
		t.Fatalf("resolved = %#v, want non-nil empty slice", resolved)
	}
}

func TestIncidentStoreListFeedRejectsNegativeTarget(t *testing.T) {
	active, resolved, err := NewIncidentStore(openIncidentTestDB(t)).ListFeed(context.Background(), -1, nil)
	if err == nil || active != nil || resolved != nil {
		t.Fatalf("ListFeed(-1) = (%v, %v, %v), want nil groups and error", active, resolved, err)
	}
}

func TestIncidentStoreListFeedUsesOneReadSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	opened := incident.Incident{ServiceID: "github", StartedAt: time.UnixMicro(1_000).UTC()}
	if err := store.Open(ctx, opened); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	active, err := listActiveIncidents(ctx, tx, "?", []any{"github"})
	if err != nil {
		t.Fatal(err)
	}

	resolvedAt := opened.StartedAt.Add(time.Second)
	opened.ResolvedAt = &resolvedAt
	if err := store.Resolve(ctx, opened); err != nil {
		t.Fatalf("resolve while read snapshot is open: %v", err)
	}
	resolved, err := listResolvedIncidents(ctx, tx, 20, "?", []any{"github"})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ServiceID != "github" || len(resolved) != 0 {
		t.Fatalf("original snapshot = (%+v active, %+v resolved), want incident only active", active, resolved)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	active, resolved, err = store.ListFeed(ctx, 20, []string{"github"})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 || len(resolved) != 1 || resolved[0].ServiceID != "github" {
		t.Fatalf("later snapshot = (%+v active, %+v resolved), want incident only resolved", active, resolved)
	}
}

func TestIncidentStoreListFeedRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := NewIncidentStore(openIncidentTestDB(t)).ListFeed(ctx, 20, []string{"github"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListFeed() error = %v, want context.Canceled", err)
	}
}

func TestIncidentStoreListFeedReportsReadFailure(t *testing.T) {
	db := openIncidentTestDB(t)
	if _, err := db.Exec("DROP TABLE incidents"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := NewIncidentStore(db).ListFeed(context.Background(), 20, []string{"github"}); err == nil || !strings.Contains(err.Error(), "query active incident feed") {
		t.Fatalf("ListFeed() error = %v, want contextual active-query failure", err)
	}
}

func TestIncidentStoreListOpenReturnsEmptyResult(t *testing.T) {
	values, err := NewIncidentStore(openIncidentTestDB(t)).ListOpen(context.Background())
	if err != nil {
		t.Fatalf("ListOpen() error = %v", err)
	}
	if values == nil || len(values) != 0 {
		t.Fatalf("ListOpen() = %#v, want non-nil empty slice", values)
	}
}

func TestIncidentStoreListOpenRestoresStoredMicrosecondsInUTC(t *testing.T) {
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
	wantStartedAt := time.UnixMicro(original.StartedAt.UnixMicro()).UTC()
	if len(values) != 1 || values[0].ServiceID != original.ServiceID ||
		values[0].StartedAt != wantStartedAt || values[0].ResolvedAt != nil {
		t.Fatalf("ListOpen() = %+v, want %q at stored microsecond %s", values, original.ServiceID, wantStartedAt)
	}
	if values[0].StartedAt.Location() != time.UTC {
		t.Fatalf("ListOpen() location = %v, want UTC", values[0].StartedAt.Location())
	}
}

func TestIncidentStoreListOpenOrdersServicesAndExcludesResolvedHistory(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	resolved := incident.Incident{ServiceID: "cloudflare", StartedAt: time.UnixMicro(1000).UTC()}
	if err := store.Open(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
	resolvedAt := resolved.StartedAt.Add(time.Second)
	resolved.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}

	for _, value := range []incident.Incident{
		{ServiceID: "openai", StartedAt: time.UnixMicro(3000).UTC()},
		{ServiceID: "github", StartedAt: time.UnixMicro(2000).UTC()},
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
		INSERT INTO incidents (service_id, started_at_us, resolved_at_us)
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
	var startedAtUS int64
	var resolvedAtUS sql.NullInt64
	if err := db.QueryRow("SELECT id, service_id, started_at_us, resolved_at_us FROM incidents").Scan(&id, &serviceID, &startedAtUS, &resolvedAtUS); err != nil {
		t.Fatal(err)
	}
	if id <= 0 || serviceID != "github" || startedAtUS != start.UnixMicro() || resolvedAtUS.Valid {
		t.Fatalf("opened row = (%d, %q, %d, %+v), want positive ID, github, %d, NULL", id, serviceID, startedAtUS, resolvedAtUS, start.UnixMicro())
	}
	assertIncidentCount(t, db, 1)

	resolvedAt := start.Add(45*time.Second + 500*time.Microsecond)
	resolved := opened
	resolved.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
	var resolvedID int64
	if err := db.QueryRow("SELECT id, resolved_at_us FROM incidents").Scan(&resolvedID, &resolvedAtUS); err != nil {
		t.Fatal(err)
	}
	if resolvedID != id || !resolvedAtUS.Valid || resolvedAtUS.Int64 != resolvedAt.UnixMicro() {
		t.Fatalf("resolved row = (%d, %+v), want (%d, %d)", resolvedID, resolvedAtUS, id, resolvedAt.UnixMicro())
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

	rows, err := db.Query("SELECT started_at_us, resolved_at_us FROM incidents WHERE service_id = ? ORDER BY id", first.ServiceID)
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
	if len(starts) != 2 || starts[0] != first.StartedAt.UnixMicro() || starts[1] != second.StartedAt.UnixMicro() ||
		!resolutions[0].Valid || resolutions[0].Int64 != resolvedAt.UnixMicro() || resolutions[1].Valid {
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
	if err := db.QueryRow("SELECT started_at_us, resolved_at_us FROM incidents").Scan(&start, &resolution); err != nil {
		t.Fatal(err)
	}
	if start != first.StartedAt.UnixMicro() || resolution.Valid {
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
	if err := db.QueryRow("SELECT COUNT(*) FROM incidents WHERE resolved_at_us IS NULL").Scan(&openCount); err != nil {
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
	if err := db.QueryRow("SELECT COUNT(*) FROM incidents WHERE resolved_at_us IS NULL").Scan(&unresolved); err != nil {
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
		if err := db.QueryRow("SELECT resolved_at_us FROM incidents WHERE service_id = ? AND started_at_us = ?", tc.value.ServiceID, tc.value.StartedAt.UnixMicro()).Scan(tc.got); err != nil {
			t.Fatal(err)
		}
	}
	if !firstResolution.Valid || firstResolution.Int64 != firstResolvedAt.UnixMicro() ||
		!secondResolution.Valid || secondResolution.Int64 != resolvedAt.UnixMicro() || otherResolution.Valid {
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
			if err := db.QueryRow("SELECT resolved_at_us FROM incidents").Scan(&resolution); err != nil {
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
	if err := db.QueryRow("SELECT started_at_us FROM incidents").Scan(&storedStart); err != nil {
		t.Fatal(err)
	}
	if storedStart != 0 {
		t.Fatalf("started_at_us = %d, want 0", storedStart)
	}
}

func TestIncidentSchemaRejectsInvalidResolutionOrder(t *testing.T) {
	for _, resolvedAtUS := range []int64{999, 1000} {
		t.Run(time.UnixMicro(resolvedAtUS).Format(time.RFC3339Nano), func(t *testing.T) {
			db := openIncidentTestDB(t)
			if _, err := db.Exec("INSERT INTO incidents (service_id, started_at_us, resolved_at_us) VALUES (?, ?, ?)", "github", 1000, resolvedAtUS); err == nil {
				t.Fatal("direct INSERT succeeded, want CHECK constraint error")
			}
			assertIncidentCount(t, db, 0)
		})
	}
}

func TestIncidentStoreAllowsResolutionWithinSameMillisecondAtDifferentMicrosecond(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	base := time.Unix(1_700_000_000, 0)
	value := incident.Incident{ServiceID: "github", StartedAt: base.Add(100 * time.Microsecond)}
	if err := store.Open(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	resolvedAt := base.Add(900 * time.Microsecond)
	if !resolvedAt.After(value.StartedAt) || resolvedAt.UnixMilli() != value.StartedAt.UnixMilli() || resolvedAt.UnixMicro() == value.StartedAt.UnixMicro() {
		t.Fatal("test timestamps must be ordered in the same millisecond but different microseconds")
	}
	value.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), value); err != nil {
		t.Fatalf("Resolve() within one millisecond: %v", err)
	}
	var resolution sql.NullInt64
	if err := db.QueryRow("SELECT resolved_at_us FROM incidents").Scan(&resolution); err != nil {
		t.Fatal(err)
	}
	if !resolution.Valid || resolution.Int64 != resolvedAt.UnixMicro() {
		t.Fatalf("stored resolution = %+v, want %d", resolution, resolvedAt.UnixMicro())
	}
}

func TestIncidentStoreSurfacesMicrosecondPrecisionCollision(t *testing.T) {
	db := openIncidentTestDB(t)
	store := NewIncidentStore(db)
	base := time.Unix(1_700_000_000, 0)
	value := incident.Incident{ServiceID: "github", StartedAt: base.Add(100 * time.Nanosecond)}
	if err := store.Open(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	resolvedAt := base.Add(900 * time.Nanosecond)
	if !resolvedAt.After(value.StartedAt) || resolvedAt.UnixMicro() != value.StartedAt.UnixMicro() {
		t.Fatal("test timestamps must be ordered in Go but equal in stored microseconds")
	}
	value.ResolvedAt = &resolvedAt
	if err := store.Resolve(context.Background(), value); err == nil {
		t.Fatal("Resolve() succeeded despite equal stored microseconds")
	}
	var resolution sql.NullInt64
	if err := db.QueryRow("SELECT resolved_at_us FROM incidents").Scan(&resolution); err != nil {
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
	if err := db.QueryRow("SELECT resolved_at_us FROM incidents").Scan(&resolution); err != nil {
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

func insertStoredIncident(t *testing.T, db *sql.DB, serviceID string, startedAtUS int64, resolvedAtUS *int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO incidents (service_id, started_at_us, resolved_at_us)
		VALUES (?, ?, ?)
	`, serviceID, startedAtUS, resolvedAtUS); err != nil {
		t.Fatalf("insert incident %q: %v", serviceID, err)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}

func assertIncidentServiceOrder(t *testing.T, values []incident.Incident, want []string) {
	t.Helper()
	if len(values) != len(want) {
		t.Fatalf("incident count = %d, want %d: %+v", len(values), len(want), values)
	}
	for index, serviceID := range want {
		if values[index].ServiceID != serviceID {
			t.Fatalf("incident %d service = %q, want %q", index, values[index].ServiceID, serviceID)
		}
	}
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
