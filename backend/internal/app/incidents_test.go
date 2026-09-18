package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/status"
)

type incidentWriterFuncs struct {
	open    func(context.Context, incident.Incident) error
	resolve func(context.Context, incident.Incident) error
}

func (w incidentWriterFuncs) Open(ctx context.Context, value incident.Incident) error {
	if w.open != nil {
		return w.open(ctx, value)
	}
	return nil
}

func (w incidentWriterFuncs) Resolve(ctx context.Context, value incident.Incident) error {
	if w.resolve != nil {
		return w.resolve(ctx, value)
	}
	return nil
}

func newTestIncidentProcessor() *IncidentProcessor {
	return NewIncidentProcessor(incident.NewLifecycle(), incidentWriterFuncs{})
}

func testIncidentUpdate(previous, current status.ServiceStatus, at time.Time) status.Update {
	return status.Update{
		ServiceID:  "example",
		Previous:   previous,
		Current:    current,
		Changed:    true,
		Applied:    true,
		ObservedAt: at,
	}
}

func TestIncidentProcessorRoutesAuthoritativeUpdates(t *testing.T) {
	start := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opened, resolved []incident.Incident
	writer := incidentWriterFuncs{
		open: func(gotCtx context.Context, value incident.Incident) error {
			if gotCtx != ctx {
				t.Fatal("Open received a different context")
			}
			opened = append(opened, value)
			return nil
		},
		resolve: func(gotCtx context.Context, value incident.Incident) error {
			if gotCtx != ctx {
				t.Fatal("Resolve received a different context")
			}
			resolved = append(resolved, value)
			return nil
		},
	}
	processor := NewIncidentProcessor(incident.NewLifecycle(), writer)
	for _, update := range []status.Update{
		{},
		{Applied: true, Changed: false, ServiceID: "example"},
		testIncidentUpdate(status.StatusUnknown, status.StatusUp, start.Add(-time.Second)),
	} {
		if err := processor.Process(ctx, update); err != nil {
			t.Fatal(err)
		}
	}
	if len(opened) != 0 || len(resolved) != 0 {
		t.Fatalf("writer called for no-action updates: opens %v, resolutions %v", opened, resolved)
	}

	if err := processor.Process(ctx, testIncidentUpdate(status.StatusUp, status.StatusDown, start)); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0].ServiceID != "example" || !opened[0].StartedAt.Equal(start) || opened[0].ResolvedAt != nil || len(resolved) != 0 {
		t.Fatalf("writer calls after opening = opens %v, resolutions %v", opened, resolved)
	}
	if err := processor.Process(ctx, testIncidentUpdate(status.StatusDown, status.StatusUp, end)); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || len(resolved) != 1 || resolved[0].ServiceID != "example" ||
		!resolved[0].StartedAt.Equal(start) || resolved[0].ResolvedAt == nil || !resolved[0].ResolvedAt.Equal(end) {
		t.Fatalf("writer calls after resolution = opens %v, resolutions %v", opened, resolved)
	}
}

func TestIncidentProcessorPreservesLifecycleError(t *testing.T) {
	called := false
	writer := incidentWriterFuncs{
		open:    func(context.Context, incident.Incident) error { called = true; return nil },
		resolve: func(context.Context, incident.Incident) error { called = true; return nil },
	}
	processor := NewIncidentProcessor(incident.NewLifecycle(), writer)
	err := processor.Process(context.Background(), testIncidentUpdate(status.StatusDown, status.StatusUp, time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)))
	if !errors.Is(err, incident.ErrNoActiveIncident) || called {
		t.Fatalf("Process() = %v, writer called %t; want lifecycle error without write", err, called)
	}
}

func TestIncidentProcessorDoesNotRollbackFailedOpen(t *testing.T) {
	wantErr := errors.New("open failed")
	lifecycle := incident.NewLifecycle()
	processor := NewIncidentProcessor(lifecycle, incidentWriterFuncs{
		open: func(context.Context, incident.Incident) error { return fmt.Errorf("writer: %w", wantErr) },
	})
	start := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	err := processor.Process(context.Background(), testIncidentUpdate(status.StatusUp, status.StatusDown, start))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Process() error = %v, want Open failure", err)
	}
	active, ok := lifecycle.Active("example")
	if !ok || !active.StartedAt.Equal(start) || active.ResolvedAt != nil {
		t.Fatalf("lifecycle after failed Open = (%+v, %t), want active incident", active, ok)
	}
}

func TestIncidentProcessorDoesNotRollbackFailedResolve(t *testing.T) {
	wantErr := errors.New("resolve failed")
	lifecycle := incident.NewLifecycle()
	processor := NewIncidentProcessor(lifecycle, incidentWriterFuncs{
		resolve: func(context.Context, incident.Incident) error { return fmt.Errorf("writer: %w", wantErr) },
	})
	start := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	if err := processor.Process(context.Background(), testIncidentUpdate(status.StatusUp, status.StatusDown, start)); err != nil {
		t.Fatal(err)
	}
	err := processor.Process(context.Background(), testIncidentUpdate(status.StatusDown, status.StatusUp, start.Add(time.Minute)))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Process() error = %v, want Resolve failure", err)
	}
	if active, ok := lifecycle.Active("example"); ok {
		t.Fatalf("lifecycle after failed Resolve = %+v, want no active incident", active)
	}
}
