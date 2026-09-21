package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/incident"
)

type fakeIncidentFeedReader struct {
	active   []incident.Incident
	resolved []incident.Incident
	err      error
	ctx      context.Context
	target   int
	calls    int
}

func (f *fakeIncidentFeedReader) ListFeed(ctx context.Context, target int) ([]incident.Incident, []incident.Incident, error) {
	f.calls++
	f.ctx = ctx
	f.target = target
	return f.active, f.resolved, f.err
}

func TestNewIncidentsQueryRequiresReader(t *testing.T) {
	query, err := NewIncidentsQuery(nil)
	if err == nil || query != nil {
		t.Fatalf("NewIncidentsQuery(nil) = (%v, %v), want nil query and error", query, err)
	}
}

func TestIncidentsQueryReturnsNonNilEmptyGroups(t *testing.T) {
	reader := &fakeIncidentFeedReader{}
	query := newIncidentsQuery(t, reader)

	got, err := query.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Active == nil || len(got.Active) != 0 {
		t.Fatalf("active = %#v, want non-nil empty slice", got.Active)
	}
	if got.Resolved == nil || len(got.Resolved) != 0 {
		t.Fatalf("resolved = %#v, want non-nil empty slice", got.Resolved)
	}
}

func TestIncidentsQueryPassesContextAndTargetOnce(t *testing.T) {
	ctx := context.WithValue(context.Background(), incidentContextKey{}, "request")
	reader := &fakeIncidentFeedReader{}
	query := newIncidentsQuery(t, reader)

	if _, err := query.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 || reader.ctx != ctx || reader.target != publicIncidentTarget {
		t.Fatalf("ListFeed calls = %d, context match = %t, target = %d; want 1, true, %d", reader.calls, reader.ctx == ctx, reader.target, publicIncidentTarget)
	}
}

func TestIncidentsQueryPreservesGroupsAndOrdering(t *testing.T) {
	base := time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC)
	firstResolvedAt := base.Add(-time.Minute)
	secondResolvedAt := base.Add(-2 * time.Minute)
	reader := &fakeIncidentFeedReader{
		active: []incident.Incident{
			{ServiceID: "new-active", StartedAt: base},
			{ServiceID: "old-active", StartedAt: base.Add(-time.Hour)},
		},
		resolved: []incident.Incident{
			{ServiceID: "new-resolved", StartedAt: base.Add(-time.Hour), ResolvedAt: &firstResolvedAt},
			{ServiceID: "old-resolved", StartedAt: base.Add(-2 * time.Hour), ResolvedAt: &secondResolvedAt},
		},
	}

	got, err := newIncidentsQuery(t, reader).Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Active) != 2 || got.Active[0].ServiceID != "new-active" || got.Active[1].ServiceID != "old-active" {
		t.Fatalf("active = %+v, want reader order", got.Active)
	}
	if len(got.Resolved) != 2 || got.Resolved[0].ServiceID != "new-resolved" || got.Resolved[1].ServiceID != "old-resolved" {
		t.Fatalf("resolved = %+v, want reader order", got.Resolved)
	}
	if got.Active[0].ResolvedAt != nil || got.Resolved[0].ResolvedAt == nil || !got.Resolved[0].ResolvedAt.Equal(firstResolvedAt) {
		t.Fatalf("mapped incidents = %+v, want preserved optional resolution times", got)
	}
}

func TestIncidentsQueryNeverTruncatesActiveResults(t *testing.T) {
	active := make([]incident.Incident, 0, publicIncidentTarget+5)
	for index := range publicIncidentTarget + 5 {
		active = append(active, incident.Incident{ServiceID: fmt.Sprintf("service-%02d", index), StartedAt: time.Unix(int64(index+1), 0)})
	}
	reader := &fakeIncidentFeedReader{active: active}

	got, err := newIncidentsQuery(t, reader).Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Active) != len(active) {
		t.Fatalf("active count = %d, want all %d", len(got.Active), len(active))
	}
}

func TestIncidentsQueryPreservesReaderError(t *testing.T) {
	wantErr := errors.New("read failed")
	reader := &fakeIncidentFeedReader{err: fmt.Errorf("storage: %w", wantErr)}

	got, err := newIncidentsQuery(t, reader).Execute(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want reader error", err)
	}
	if got.Active != nil || got.Resolved != nil {
		t.Fatalf("Execute() view = %+v, want zero view on error", got)
	}
}

func TestIncidentsQueryReturnsIndependentResolutionTimes(t *testing.T) {
	resolvedAt := time.Date(2026, 9, 22, 2, 0, 0, 0, time.UTC)
	reader := &fakeIncidentFeedReader{resolved: []incident.Incident{{
		ServiceID:  "github",
		StartedAt:  resolvedAt.Add(-time.Hour),
		ResolvedAt: &resolvedAt,
	}}}
	query := newIncidentsQuery(t, reader)

	first, err := query.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Resolved[0].ResolvedAt == reader.resolved[0].ResolvedAt {
		t.Fatal("application view aliases reader resolution pointer")
	}
	changed := resolvedAt.Add(time.Hour)
	*first.Resolved[0].ResolvedAt = changed
	first.Resolved[0].ServiceID = "changed"

	second, err := query.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reader.resolved[0].ServiceID != "github" || !reader.resolved[0].ResolvedAt.Equal(resolvedAt) ||
		second.Resolved[0].ServiceID != "github" || !second.Resolved[0].ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("mutation leaked: reader %+v, second %+v", reader.resolved, second.Resolved)
	}
}

type incidentContextKey struct{}

func newIncidentsQuery(t *testing.T, reader IncidentFeedReader) *IncidentsQuery {
	t.Helper()
	query, err := NewIncidentsQuery(reader)
	if err != nil {
		t.Fatal(err)
	}
	return query
}
