package incident

import (
	"errors"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/status"
)

var incidentTestTime = time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)

func TestLifecycleOpensOnTransitionToDown(t *testing.T) {
	for _, previous := range []status.ServiceStatus{status.StatusUnknown, status.StatusUp} {
		t.Run(string(previous), func(t *testing.T) {
			lifecycle := NewLifecycle()
			opened, err := lifecycle.Apply(transition("github", previous, status.StatusDown, incidentTestTime))
			if err != nil {
				t.Fatal(err)
			}
			assertOpenIncident(t, opened, "github", incidentTestTime)
			active, ok := lifecycle.Active("github")
			if !ok {
				t.Fatal("opened incident is not active")
			}
			assertOpenIncident(t, &active, "github", incidentTestTime)
		})
	}
}

func TestLifecycleKeepsOneIncidentThroughUnknownAndReentryToDown(t *testing.T) {
	lifecycle := NewLifecycle()
	opened, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, incidentTestTime))
	if err != nil {
		t.Fatal(err)
	}
	assertOpenIncident(t, opened, "github", incidentTestTime)

	for _, update := range []status.Update{
		transition("github", status.StatusDown, status.StatusUnknown, incidentTestTime.Add(time.Second)),
		transition("github", status.StatusUnknown, status.StatusDown, incidentTestTime.Add(2*time.Second)),
		transition("github", status.StatusUp, status.StatusDown, incidentTestTime.Add(3*time.Second)),
	} {
		result, err := lifecycle.Apply(update)
		if err != nil || result != nil {
			t.Fatalf("Apply(%q to %q) = (%+v, %v), want no action", update.Previous, update.Current, result, err)
		}
		active, ok := lifecycle.Active("github")
		if !ok {
			t.Fatal("original incident is no longer active")
		}
		assertOpenIncident(t, &active, "github", incidentTestTime)
	}
}

func TestLifecycleResolvesOnConfirmedUp(t *testing.T) {
	tests := []struct {
		name       string
		throughUnk bool
	}{
		{name: "direct DOWN to UP"},
		{name: "DOWN through UNKNOWN to UP", throughUnk: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := NewLifecycle()
			opened, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, incidentTestTime))
			if err != nil {
				t.Fatal(err)
			}
			assertOpenIncident(t, opened, "github", incidentTestTime)

			previous := status.StatusDown
			if tt.throughUnk {
				result, err := lifecycle.Apply(transition("github", status.StatusDown, status.StatusUnknown, incidentTestTime.Add(time.Second)))
				if err != nil || result != nil {
					t.Fatalf("DOWN to UNKNOWN = (%+v, %v), want no action", result, err)
				}
				active, ok := lifecycle.Active("github")
				if !ok {
					t.Fatal("incident closed while status was UNKNOWN")
				}
				assertOpenIncident(t, &active, "github", incidentTestTime)
				previous = status.StatusUnknown
			}

			recoveredAt := incidentTestTime.Add(2 * time.Second)
			resolved, err := lifecycle.Apply(transition("github", previous, status.StatusUp, recoveredAt))
			if err != nil {
				t.Fatal(err)
			}
			if resolved == nil || resolved.ServiceID != opened.ServiceID || !resolved.StartedAt.Equal(opened.StartedAt) || resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(recoveredAt) {
				t.Fatalf("resolved incident = %+v, want original outage resolved at %s", resolved, recoveredAt)
			}
			if _, ok := lifecycle.Active("github"); ok {
				t.Fatal("resolved incident remains active")
			}
		})
	}
}

func TestLifecycleRecoveryWithoutActiveIncident(t *testing.T) {
	lifecycle := NewLifecycle()
	result, err := lifecycle.Apply(transition("github", status.StatusUnknown, status.StatusUp, incidentTestTime))
	if result != nil || err != nil {
		t.Fatalf("UNKNOWN to UP = (%+v, %v), want no action", result, err)
	}
	result, err = lifecycle.Apply(transition("github", status.StatusDown, status.StatusUp, incidentTestTime.Add(time.Second)))
	if result != nil || !errors.Is(err, ErrNoActiveIncident) {
		t.Fatalf("DOWN to UP = (%+v, %v), want ErrNoActiveIncident", result, err)
	}
	if _, ok := lifecycle.Active("github"); ok {
		t.Fatal("recovery without active incident created state")
	}
}

func TestLifecycleImmediatelyIgnoresUnappliedOrUnchangedUpdates(t *testing.T) {
	lifecycle := NewLifecycle()
	opened, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, incidentTestTime))
	if err != nil {
		t.Fatal(err)
	}
	assertOpenIncident(t, opened, "github", incidentTestTime)

	for _, update := range []status.Update{
		{Applied: false, Changed: true, Previous: status.ServiceStatus("invalid"), Current: status.StatusUp},
		{Applied: true, Changed: false, Previous: status.StatusDown, Current: status.StatusUp},
		{},
	} {
		result, err := lifecycle.Apply(update)
		if result != nil || err != nil {
			t.Fatalf("Apply(%+v) = (%+v, %v), want immediate no-op", update, result, err)
		}
		active, ok := lifecycle.Active("github")
		if !ok {
			t.Fatal("no-op update removed active incident")
		}
		assertOpenIncident(t, &active, "github", incidentTestTime)
	}
}

func TestLifecycleRejectsInvalidConfirmedTransitions(t *testing.T) {
	tests := []struct {
		name   string
		update status.Update
	}{
		{name: "empty service ID", update: transition("", status.StatusUp, status.StatusDown, incidentTestTime.Add(time.Second))},
		{name: "zero observation time", update: transition("github", status.StatusUp, status.StatusDown, time.Time{})},
		{name: "invalid previous status", update: transition("github", status.ServiceStatus("invalid"), status.StatusDown, incidentTestTime.Add(time.Second))},
		{name: "invalid current status", update: transition("github", status.StatusUp, status.ServiceStatus("invalid"), incidentTestTime.Add(time.Second))},
		{name: "same previous and current", update: transition("github", status.StatusDown, status.StatusDown, incidentTestTime.Add(time.Second))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := NewLifecycle()
			if _, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, incidentTestTime)); err != nil {
				t.Fatal(err)
			}
			result, err := lifecycle.Apply(tt.update)
			if result != nil || !errors.Is(err, ErrInvalidUpdate) {
				t.Fatalf("Apply(%+v) = (%+v, %v), want ErrInvalidUpdate", tt.update, result, err)
			}
			active, ok := lifecycle.Active("github")
			if !ok {
				t.Fatal("invalid update removed active incident")
			}
			assertOpenIncident(t, &active, "github", incidentTestTime)
			if len(lifecycle.active) != 1 {
				t.Fatalf("active incident count = %d, want 1", len(lifecycle.active))
			}
		})
	}
}

func TestLifecycleRejectsResolutionAtOrBeforeStart(t *testing.T) {
	for _, recoveredAt := range []time.Time{incidentTestTime.Add(-time.Second), incidentTestTime} {
		t.Run(recoveredAt.Format(time.RFC3339Nano), func(t *testing.T) {
			lifecycle := NewLifecycle()
			if _, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, incidentTestTime)); err != nil {
				t.Fatal(err)
			}
			result, err := lifecycle.Apply(transition("github", status.StatusDown, status.StatusUp, recoveredAt))
			if result != nil || !errors.Is(err, ErrInvalidUpdate) {
				t.Fatalf("recovery at %s = (%+v, %v), want ErrInvalidUpdate", recoveredAt, result, err)
			}
			active, ok := lifecycle.Active("github")
			if !ok {
				t.Fatal("invalid recovery removed active incident")
			}
			assertOpenIncident(t, &active, "github", incidentTestTime)

			validAt := incidentTestTime.Add(time.Second)
			resolved, err := lifecycle.Apply(transition("github", status.StatusDown, status.StatusUp, validAt))
			if err != nil || resolved == nil || resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(validAt) {
				t.Fatalf("valid recovery after invalid one = (%+v, %v)", resolved, err)
			}
			if _, ok := lifecycle.Active("github"); ok {
				t.Fatal("valid recovery left incident active")
			}
		})
	}
}

func TestLifecycleKeepsServicesIndependent(t *testing.T) {
	lifecycle := NewLifecycle()
	if _, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, incidentTestTime)); err != nil {
		t.Fatal(err)
	}
	otherStart := incidentTestTime.Add(time.Second)
	if _, err := lifecycle.Apply(transition("openai", status.StatusUnknown, status.StatusDown, otherStart)); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Apply(transition("github", status.StatusDown, status.StatusUp, incidentTestTime.Add(2*time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, ok := lifecycle.Active("github"); ok {
		t.Fatal("recovered service remains active")
	}
	other, ok := lifecycle.Active("openai")
	if !ok {
		t.Fatal("unrelated service incident was removed")
	}
	assertOpenIncident(t, &other, "openai", otherStart)
}

func TestLifecycleCanOpenNewIncidentAfterRecovery(t *testing.T) {
	lifecycle := NewLifecycle()
	first, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, incidentTestTime))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Apply(transition("github", status.StatusDown, status.StatusUp, incidentTestTime.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	secondStart := incidentTestTime.Add(2 * time.Second)
	second, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, secondStart))
	if err != nil {
		t.Fatal(err)
	}
	assertOpenIncident(t, second, "github", secondStart)
	if first.StartedAt.Equal(second.StartedAt) {
		t.Fatal("new outage reused the previous incident start time")
	}
	active, ok := lifecycle.Active("github")
	if !ok {
		t.Fatal("new outage is not active")
	}
	assertOpenIncident(t, &active, "github", secondStart)
}

func TestLifecycleReturnsIndependentSnapshots(t *testing.T) {
	lifecycle := NewLifecycle()
	opened, err := lifecycle.Apply(transition("github", status.StatusUp, status.StatusDown, incidentTestTime))
	if err != nil {
		t.Fatal(err)
	}
	opened.ServiceID = "changed"
	opened.StartedAt = incidentTestTime.Add(time.Hour)
	resolvedAt := incidentTestTime.Add(2 * time.Hour)
	opened.ResolvedAt = &resolvedAt

	active, ok := lifecycle.Active("github")
	if !ok {
		t.Fatal("mutating Apply result changed active lookup")
	}
	assertOpenIncident(t, &active, "github", incidentTestTime)
	active.ServiceID = "changed again"
	active.StartedAt = incidentTestTime.Add(3 * time.Hour)
	active.ResolvedAt = &resolvedAt

	again, ok := lifecycle.Active("github")
	if !ok {
		t.Fatal("mutating Active result removed active incident")
	}
	assertOpenIncident(t, &again, "github", incidentTestTime)
}

func transition(serviceID string, previous, current status.ServiceStatus, at time.Time) status.Update {
	return status.Update{
		ServiceID:  serviceID,
		Previous:   previous,
		Current:    current,
		Changed:    true,
		ObservedAt: at,
		Applied:    true,
	}
}

func assertOpenIncident(t *testing.T, incident *Incident, serviceID string, startedAt time.Time) {
	t.Helper()
	if incident == nil || incident.ServiceID != serviceID || !incident.StartedAt.Equal(startedAt) || incident.ResolvedAt != nil {
		t.Fatalf("incident = %+v, want open %q started at %s", incident, serviceID, startedAt)
	}
}
