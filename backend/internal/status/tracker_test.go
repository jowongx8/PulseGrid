package status

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
)

var testBaseTime = time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)

func TestNewTrackerInitializesEnabledServices(t *testing.T) {
	services := []service.Service{
		testService("disabled", false),
		testService("github", true),
		testService("cloudflare", true),
	}

	tracker, err := NewTracker(services)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}

	services[1].ID = "mutated"

	snapshots := tracker.Snapshot()
	if len(snapshots) != 2 {
		t.Fatalf("Snapshot() length = %d, want 2", len(snapshots))
	}

	wantOrder := []string{"github", "cloudflare"}
	for index, snapshot := range snapshots {
		if snapshot.ServiceID != wantOrder[index] {
			t.Fatalf("Snapshot()[%d].ServiceID = %q, want %q", index, snapshot.ServiceID, wantOrder[index])
		}
		if snapshot.Status != StatusUnknown {
			t.Fatalf("Snapshot()[%d].Status = %q, want %q", index, snapshot.Status, StatusUnknown)
		}
		if !snapshot.LastObservedAt.IsZero() {
			t.Fatalf("Snapshot()[%d].LastObservedAt = %s, want zero", index, snapshot.LastObservedAt)
		}
		if !snapshot.StatusChangedAt.IsZero() {
			t.Fatalf("Snapshot()[%d].StatusChangedAt = %s, want zero", index, snapshot.StatusChangedAt)
		}
	}

	if _, err := tracker.Get("disabled"); !errors.Is(err, ErrUnknownService) {
		t.Fatalf("Get(disabled) error = %v, want ErrUnknownService", err)
	}
}

func TestNewTrackerValidation(t *testing.T) {
	tests := []struct {
		name     string
		services []service.Service
		wantErr  error
	}{
		{
			name:     "zero enabled services",
			services: []service.Service{testService("disabled", false)},
			wantErr:  ErrNoEnabledServices,
		},
		{
			name:     "empty enabled service ID",
			services: []service.Service{{Enabled: true}},
			wantErr:  ErrInvalidServiceID,
		},
		{
			name: "duplicate enabled service ID",
			services: []service.Service{
				testService("github", true),
				testService("github", true),
			},
			wantErr: ErrDuplicateServiceID,
		},
		{
			name: "duplicate disabled service ID is ignored",
			services: []service.Service{
				testService("github", true),
				testService("github", false),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker, err := NewTracker(tt.services)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewTracker() error = %v, want %v", err, tt.wantErr)
				}
				if tracker != nil {
					t.Fatalf("NewTracker() tracker = %#v, want nil on error", tracker)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewTracker() error = %v", err)
			}
			if tracker == nil {
				t.Fatal("NewTracker() tracker = nil, want tracker")
			}
		})
	}
}

func TestTrackerUnknownTransitions(t *testing.T) {
	t.Run("healthy transitions immediately to up", func(t *testing.T) {
		tracker := newTestTracker(t, "github")
		observedAt := testBaseTime.Add(time.Second)

		update := applyObservation(t, tracker, "github", monitoring.ObservationHealthy, observedAt)
		assertUpdate(t, update, Update{
			ServiceID:  "github",
			Previous:   StatusUnknown,
			Current:    StatusUp,
			Changed:    true,
			ObservedAt: observedAt,
			Applied:    true,
		})

		assertSnapshot(t, tracker, "github", ServiceSnapshot{
			ServiceID:       "github",
			Status:          StatusUp,
			LastObservedAt:  observedAt,
			StatusChangedAt: observedAt,
		})
	})

	t.Run("three failures transition to down", func(t *testing.T) {
		tracker := newTestTracker(t, "github")
		first := testBaseTime.Add(time.Second)
		second := testBaseTime.Add(2 * time.Second)
		third := testBaseTime.Add(3 * time.Second)

		assertUpdate(t, applyObservation(t, tracker, "github", monitoring.ObservationFailure, first), Update{
			ServiceID:  "github",
			Previous:   StatusUnknown,
			Current:    StatusUnknown,
			ObservedAt: first,
			Applied:    true,
		})
		assertUpdate(t, applyObservation(t, tracker, "github", monitoring.ObservationFailure, second), Update{
			ServiceID:  "github",
			Previous:   StatusUnknown,
			Current:    StatusUnknown,
			ObservedAt: second,
			Applied:    true,
		})
		assertUpdate(t, applyObservation(t, tracker, "github", monitoring.ObservationFailure, third), Update{
			ServiceID:  "github",
			Previous:   StatusUnknown,
			Current:    StatusDown,
			Changed:    true,
			ObservedAt: third,
			Applied:    true,
		})

		assertSnapshot(t, tracker, "github", ServiceSnapshot{
			ServiceID:       "github",
			Status:          StatusDown,
			LastObservedAt:  third,
			StatusChangedAt: third,
		})
	})

	t.Run("indeterminate stays unknown and updates last observed", func(t *testing.T) {
		tracker := newTestTracker(t, "github")
		observedAt := testBaseTime.Add(time.Second)

		update := applyObservation(t, tracker, "github", monitoring.ObservationIndeterminate, observedAt)
		assertUpdate(t, update, Update{
			ServiceID:  "github",
			Previous:   StatusUnknown,
			Current:    StatusUnknown,
			ObservedAt: observedAt,
			Applied:    true,
		})

		assertSnapshot(t, tracker, "github", ServiceSnapshot{
			ServiceID:       "github",
			Status:          StatusUnknown,
			LastObservedAt:  observedAt,
			StatusChangedAt: time.Time{},
		})
	})

	t.Run("ignored is a no-op", func(t *testing.T) {
		tracker := newTestTracker(t, "github")
		observedAt := testBaseTime.Add(time.Second)

		update := applyObservation(t, tracker, "github", monitoring.ObservationIgnored, observedAt)
		assertUpdate(t, update, Update{
			ServiceID:  "github",
			Previous:   StatusUnknown,
			Current:    StatusUnknown,
			ObservedAt: observedAt,
		})

		assertSnapshot(t, tracker, "github", ServiceSnapshot{
			ServiceID: "github",
			Status:    StatusUnknown,
		})
	})
}

func TestTrackerUpTransitions(t *testing.T) {
	t.Run("healthy keeps up without rewriting status changed time", func(t *testing.T) {
		tracker := newTestTracker(t, "github")
		transitionAt := testBaseTime.Add(time.Second)
		nextObservedAt := testBaseTime.Add(2 * time.Second)
		applyObservation(t, tracker, "github", monitoring.ObservationHealthy, transitionAt)

		update := applyObservation(t, tracker, "github", monitoring.ObservationHealthy, nextObservedAt)
		assertUpdate(t, update, Update{
			ServiceID:  "github",
			Previous:   StatusUp,
			Current:    StatusUp,
			ObservedAt: nextObservedAt,
			Applied:    true,
		})

		assertSnapshot(t, tracker, "github", ServiceSnapshot{
			ServiceID:       "github",
			Status:          StatusUp,
			LastObservedAt:  nextObservedAt,
			StatusChangedAt: transitionAt,
		})
	})

	t.Run("three failures transition to down", func(t *testing.T) {
		tracker := trackerInStatus(t, StatusUp)
		times := sequentialTimes(3, 2*time.Second)

		assertStatusAfter(t, tracker, monitoring.ObservationFailure, times[0], StatusUp, false)
		assertStatusAfter(t, tracker, monitoring.ObservationFailure, times[1], StatusUp, false)
		assertStatusAfter(t, tracker, monitoring.ObservationFailure, times[2], StatusDown, true)
	})

	t.Run("three indeterminate observations transition to unknown", func(t *testing.T) {
		tracker := trackerInStatus(t, StatusUp)
		times := sequentialTimes(3, 2*time.Second)

		assertStatusAfter(t, tracker, monitoring.ObservationIndeterminate, times[0], StatusUp, false)
		assertStatusAfter(t, tracker, monitoring.ObservationIndeterminate, times[1], StatusUp, false)
		assertStatusAfter(t, tracker, monitoring.ObservationIndeterminate, times[2], StatusUnknown, true)
	})

	t.Run("ignored is a no-op", func(t *testing.T) {
		tracker := trackerInStatus(t, StatusUp)
		before := getSnapshot(t, tracker, "github")
		observedAt := testBaseTime.Add(2 * time.Second)

		update := applyObservation(t, tracker, "github", monitoring.ObservationIgnored, observedAt)
		assertUpdate(t, update, Update{
			ServiceID:  "github",
			Previous:   StatusUp,
			Current:    StatusUp,
			ObservedAt: observedAt,
		})
		assertSnapshot(t, tracker, "github", before)
	})
}

func TestTrackerDownTransitions(t *testing.T) {
	t.Run("failure keeps down", func(t *testing.T) {
		tracker := trackerInStatus(t, StatusDown)
		before := getSnapshot(t, tracker, "github")
		observedAt := testBaseTime.Add(4 * time.Second)

		update := applyObservation(t, tracker, "github", monitoring.ObservationFailure, observedAt)
		assertUpdate(t, update, Update{
			ServiceID:  "github",
			Previous:   StatusDown,
			Current:    StatusDown,
			ObservedAt: observedAt,
			Applied:    true,
		})

		want := before
		want.LastObservedAt = observedAt
		assertSnapshot(t, tracker, "github", want)
	})

	t.Run("two healthy observations recover to up", func(t *testing.T) {
		tracker := trackerInStatus(t, StatusDown)
		times := sequentialTimes(2, 4*time.Second)

		assertStatusAfter(t, tracker, monitoring.ObservationHealthy, times[0], StatusDown, false)
		assertStatusAfter(t, tracker, monitoring.ObservationHealthy, times[1], StatusUp, true)
	})

	t.Run("three indeterminate observations transition to unknown", func(t *testing.T) {
		tracker := trackerInStatus(t, StatusDown)
		times := sequentialTimes(3, 4*time.Second)

		assertStatusAfter(t, tracker, monitoring.ObservationIndeterminate, times[0], StatusDown, false)
		assertStatusAfter(t, tracker, monitoring.ObservationIndeterminate, times[1], StatusDown, false)
		assertStatusAfter(t, tracker, monitoring.ObservationIndeterminate, times[2], StatusUnknown, true)
	})

	t.Run("ignored is a no-op", func(t *testing.T) {
		tracker := trackerInStatus(t, StatusDown)
		before := getSnapshot(t, tracker, "github")
		observedAt := testBaseTime.Add(4 * time.Second)

		update := applyObservation(t, tracker, "github", monitoring.ObservationIgnored, observedAt)
		assertUpdate(t, update, Update{
			ServiceID:  "github",
			Previous:   StatusDown,
			Current:    StatusDown,
			ObservedAt: observedAt,
		})
		assertSnapshot(t, tracker, "github", before)
	})
}

func TestTrackerStreakBreaking(t *testing.T) {
	tests := []struct {
		name     string
		initial  ServiceStatus
		sequence []monitoring.ObservationKind
		want     ServiceStatus
	}{
		{
			name:     "healthy breaks failure streak",
			initial:  StatusUnknown,
			sequence: []monitoring.ObservationKind{monitoring.ObservationFailure, monitoring.ObservationHealthy, monitoring.ObservationFailure, monitoring.ObservationFailure},
			want:     StatusUp,
		},
		{
			name:     "indeterminate breaks failure streak",
			initial:  StatusUnknown,
			sequence: []monitoring.ObservationKind{monitoring.ObservationFailure, monitoring.ObservationIndeterminate, monitoring.ObservationFailure, monitoring.ObservationFailure},
			want:     StatusUnknown,
		},
		{
			name:     "indeterminate breaks down recovery streak",
			initial:  StatusDown,
			sequence: []monitoring.ObservationKind{monitoring.ObservationHealthy, monitoring.ObservationIndeterminate, monitoring.ObservationHealthy},
			want:     StatusDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := trackerInStatus(t, tt.initial)
			startOffset := 1
			if tt.initial == StatusDown {
				startOffset = 4
			}

			for index, kind := range tt.sequence {
				applyObservation(t, tracker, "github", kind, testBaseTime.Add(time.Duration(startOffset+index)*time.Second))
			}

			snapshot := getSnapshot(t, tracker, "github")
			if snapshot.Status != tt.want {
				t.Fatalf("final status = %q, want %q", snapshot.Status, tt.want)
			}
		})
	}
}

func TestTrackerIgnoredPreservesStreaksAndTimestamps(t *testing.T) {
	tracker := newTestTracker(t, "github")
	first := testBaseTime.Add(time.Second)
	ignoredAt := testBaseTime.Add(2 * time.Second)
	second := testBaseTime.Add(3 * time.Second)
	third := testBaseTime.Add(4 * time.Second)

	applyObservation(t, tracker, "github", monitoring.ObservationFailure, first)
	beforeIgnored := getSnapshot(t, tracker, "github")

	update := applyObservation(t, tracker, "github", monitoring.ObservationIgnored, ignoredAt)
	assertUpdate(t, update, Update{
		ServiceID:  "github",
		Previous:   StatusUnknown,
		Current:    StatusUnknown,
		ObservedAt: ignoredAt,
	})
	assertSnapshot(t, tracker, "github", beforeIgnored)

	applyObservation(t, tracker, "github", monitoring.ObservationFailure, second)
	update = applyObservation(t, tracker, "github", monitoring.ObservationFailure, third)
	assertUpdate(t, update, Update{
		ServiceID:  "github",
		Previous:   StatusUnknown,
		Current:    StatusDown,
		Changed:    true,
		ObservedAt: third,
		Applied:    true,
	})
}

func TestTrackerStaleAndDuplicateObservationsAreNoops(t *testing.T) {
	tracker := newTestTracker(t, "github")
	acceptedAt := testBaseTime.Add(2 * time.Second)
	olderAt := testBaseTime.Add(time.Second)
	applyObservation(t, tracker, "github", monitoring.ObservationHealthy, acceptedAt)
	before := getSnapshot(t, tracker, "github")

	update := applyObservation(t, tracker, "github", monitoring.ObservationFailure, olderAt)
	assertUpdate(t, update, Update{
		ServiceID:  "github",
		Previous:   StatusUp,
		Current:    StatusUp,
		ObservedAt: olderAt,
	})
	assertSnapshot(t, tracker, "github", before)

	update = applyObservation(t, tracker, "github", monitoring.ObservationFailure, acceptedAt)
	assertUpdate(t, update, Update{
		ServiceID:  "github",
		Previous:   StatusUp,
		Current:    StatusUp,
		ObservedAt: acceptedAt,
	})
	assertSnapshot(t, tracker, "github", before)
}

func TestTrackerStaleAndDuplicateObservationsDoNotBreakStreaks(t *testing.T) {
	tests := []struct {
		name string
		kind monitoring.ObservationKind
		at   time.Time
	}{
		{
			name: "stale observation",
			kind: monitoring.ObservationHealthy,
			at:   testBaseTime.Add(time.Second),
		},
		{
			name: "duplicate observation",
			kind: monitoring.ObservationIndeterminate,
			at:   testBaseTime.Add(2 * time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTestTracker(t, "github")
			first := testBaseTime.Add(2 * time.Second)
			second := testBaseTime.Add(3 * time.Second)
			third := testBaseTime.Add(4 * time.Second)
			applyObservation(t, tracker, "github", monitoring.ObservationFailure, first)

			update := applyObservation(t, tracker, "github", tt.kind, tt.at)
			assertUpdate(t, update, Update{
				ServiceID:  "github",
				Previous:   StatusUnknown,
				Current:    StatusUnknown,
				ObservedAt: tt.at,
			})

			applyObservation(t, tracker, "github", monitoring.ObservationFailure, second)
			update = applyObservation(t, tracker, "github", monitoring.ObservationFailure, third)
			assertUpdate(t, update, Update{
				ServiceID:  "github",
				Previous:   StatusUnknown,
				Current:    StatusDown,
				Changed:    true,
				ObservedAt: third,
				Applied:    true,
			})
		})
	}
}

func TestTrackerRejectsInvalidObservationsWithoutMutation(t *testing.T) {
	tests := []struct {
		name        string
		observation monitoring.Observation
		wantErr     error
	}{
		{
			name: "unknown service",
			observation: monitoring.Observation{
				ServiceID:  "unknown",
				ObservedAt: testBaseTime.Add(time.Second),
				Kind:       monitoring.ObservationHealthy,
			},
			wantErr: ErrUnknownService,
		},
		{
			name: "zero timestamp",
			observation: monitoring.Observation{
				ServiceID: "github",
				Kind:      monitoring.ObservationHealthy,
			},
			wantErr: ErrInvalidObservationTime,
		},
		{
			name: "invalid observation kind",
			observation: monitoring.Observation{
				ServiceID:  "github",
				ObservedAt: testBaseTime.Add(time.Second),
				Kind:       monitoring.ObservationKind("unexpected"),
			},
			wantErr: ErrInvalidObservationKind,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTestTracker(t, "github")
			before := tracker.Snapshot()

			update, err := tracker.Apply(tt.observation)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply() error = %v, want %v", err, tt.wantErr)
			}
			if update != (Update{}) {
				t.Fatalf("Apply() update = %+v, want zero update on error", update)
			}

			after := tracker.Snapshot()
			if !equalSnapshots(after, before) {
				t.Fatalf("Snapshot() after error = %+v, want %+v", after, before)
			}
		})
	}
}

func TestTrackerGetUnknownService(t *testing.T) {
	tracker := newTestTracker(t, "github")

	if _, err := tracker.Get("unknown"); !errors.Is(err, ErrUnknownService) {
		t.Fatalf("Get() error = %v, want ErrUnknownService", err)
	}
}

func TestTrackerSnapshotReturnsValueCopiesInConstructorOrder(t *testing.T) {
	tracker := newTestTracker(t, "github", "cloudflare")
	githubAt := testBaseTime.Add(time.Second)
	cloudflareAt := testBaseTime.Add(2 * time.Second)
	applyObservation(t, tracker, "github", monitoring.ObservationHealthy, githubAt)
	applyObservation(t, tracker, "cloudflare", monitoring.ObservationFailure, cloudflareAt)

	snapshots := tracker.Snapshot()
	if len(snapshots) != 2 {
		t.Fatalf("Snapshot() length = %d, want 2", len(snapshots))
	}
	if snapshots[0].ServiceID != "github" || snapshots[1].ServiceID != "cloudflare" {
		t.Fatalf("Snapshot() order = [%q, %q], want [github, cloudflare]", snapshots[0].ServiceID, snapshots[1].ServiceID)
	}

	snapshots[0] = ServiceSnapshot{ServiceID: "mutated", Status: StatusDown}
	next := tracker.Snapshot()
	if next[0].ServiceID != "github" || next[0].Status != StatusUp {
		t.Fatalf("Snapshot() exposed mutable state: got %+v", next[0])
	}

	got, err := tracker.Get("github")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ServiceID != "github" || got.Status != StatusUp || got.LastObservedAt != githubAt {
		t.Fatalf("Get() snapshot = %+v, want github up at %s", got, githubAt)
	}
}

func TestTrackerConcurrentAccessIsRaceSafe(t *testing.T) {
	const serviceCount = 8
	services := make([]service.Service, 0, serviceCount)
	for index := range serviceCount {
		services = append(services, testService(fmt.Sprintf("service-%d", index), true))
	}

	tracker, err := NewTracker(services)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, serviceCount)
	start := make(chan struct{})

	for index, svc := range services {
		wg.Add(1)
		go func(index int, serviceID string) {
			defer wg.Done()
			<-start

			kinds := []monitoring.ObservationKind{
				monitoring.ObservationHealthy,
				monitoring.ObservationFailure,
				monitoring.ObservationFailure,
				monitoring.ObservationFailure,
				monitoring.ObservationIndeterminate,
			}
			for step, kind := range kinds {
				observedAt := testBaseTime.Add(time.Duration(index)*time.Minute + time.Duration(step+1)*time.Second)
				if _, err := tracker.Apply(newObservation(serviceID, kind, observedAt)); err != nil {
					errs <- err
					return
				}
				if _, err := tracker.Get(serviceID); err != nil {
					errs <- err
					return
				}
				_ = tracker.Snapshot()
			}
		}(index, svc.ID)
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent access error = %v", err)
		}
	}
}

func trackerInStatus(t *testing.T, target ServiceStatus) *Tracker {
	t.Helper()

	tracker := newTestTracker(t, "github")
	switch target {
	case StatusUnknown:
		return tracker
	case StatusUp:
		applyObservation(t, tracker, "github", monitoring.ObservationHealthy, testBaseTime.Add(time.Second))
		return tracker
	case StatusDown:
		applyObservation(t, tracker, "github", monitoring.ObservationFailure, testBaseTime.Add(time.Second))
		applyObservation(t, tracker, "github", monitoring.ObservationFailure, testBaseTime.Add(2*time.Second))
		applyObservation(t, tracker, "github", monitoring.ObservationFailure, testBaseTime.Add(3*time.Second))
		return tracker
	default:
		t.Fatalf("unsupported target status %q", target)
		return nil
	}
}

func assertStatusAfter(t *testing.T, tracker *Tracker, kind monitoring.ObservationKind, observedAt time.Time, want ServiceStatus, wantChanged bool) {
	t.Helper()

	before := getSnapshot(t, tracker, "github")
	update := applyObservation(t, tracker, "github", kind, observedAt)
	assertUpdate(t, update, Update{
		ServiceID:  "github",
		Previous:   before.Status,
		Current:    want,
		Changed:    wantChanged,
		ObservedAt: observedAt,
		Applied:    true,
	})

	snapshot := getSnapshot(t, tracker, "github")
	if snapshot.Status != want {
		t.Fatalf("status after %q = %q, want %q", kind, snapshot.Status, want)
	}
	if snapshot.LastObservedAt != observedAt {
		t.Fatalf("LastObservedAt after %q = %s, want %s", kind, snapshot.LastObservedAt, observedAt)
	}
	if wantChanged && snapshot.StatusChangedAt != observedAt {
		t.Fatalf("StatusChangedAt after %q = %s, want %s", kind, snapshot.StatusChangedAt, observedAt)
	}
	if !wantChanged && snapshot.StatusChangedAt != before.StatusChangedAt {
		t.Fatalf("StatusChangedAt after %q = %s, want unchanged %s", kind, snapshot.StatusChangedAt, before.StatusChangedAt)
	}
}

func newTestTracker(t *testing.T, serviceIDs ...string) *Tracker {
	t.Helper()

	services := make([]service.Service, 0, len(serviceIDs))
	for _, serviceID := range serviceIDs {
		services = append(services, testService(serviceID, true))
	}

	tracker, err := NewTracker(services)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}

	return tracker
}

func testService(id string, enabled bool) service.Service {
	return service.Service{
		ID:      id,
		Enabled: enabled,
	}
}

func applyObservation(t *testing.T, tracker *Tracker, serviceID string, kind monitoring.ObservationKind, observedAt time.Time) Update {
	t.Helper()

	update, err := tracker.Apply(newObservation(serviceID, kind, observedAt))
	if err != nil {
		t.Fatalf("Apply(%q, %q, %s) error = %v", serviceID, kind, observedAt, err)
	}

	return update
}

func newObservation(serviceID string, kind monitoring.ObservationKind, observedAt time.Time) monitoring.Observation {
	return monitoring.Observation{
		ServiceID:  serviceID,
		ObservedAt: observedAt,
		Kind:       kind,
	}
}

func getSnapshot(t *testing.T, tracker *Tracker, serviceID string) ServiceSnapshot {
	t.Helper()

	snapshot, err := tracker.Get(serviceID)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", serviceID, err)
	}

	return snapshot
}

func assertSnapshot(t *testing.T, tracker *Tracker, serviceID string, want ServiceSnapshot) {
	t.Helper()

	got := getSnapshot(t, tracker, serviceID)
	if got != want {
		t.Fatalf("Get(%q) = %+v, want %+v", serviceID, got, want)
	}
}

func assertUpdate(t *testing.T, got Update, want Update) {
	t.Helper()

	if got != want {
		t.Fatalf("Update = %+v, want %+v", got, want)
	}
}

func sequentialTimes(count int, startOffset time.Duration) []time.Time {
	times := make([]time.Time, count)
	for index := range count {
		times[index] = testBaseTime.Add(startOffset + time.Duration(index)*time.Second)
	}
	return times
}

func equalSnapshots(a, b []ServiceSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
