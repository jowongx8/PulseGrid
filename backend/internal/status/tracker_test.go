package status

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/monitoring"
)

var testBaseTime = time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)

func TestNewTrackerSeparatesConfiguredFromObservedServices(t *testing.T) {
	serviceIDs := []string{"github", "cloudflare"}
	tracker, err := NewTracker(serviceIDs)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	serviceIDs[0] = "mutated"

	snapshots := tracker.Snapshot()
	if snapshots == nil || len(snapshots) != 0 {
		t.Fatalf("Snapshot() = %#v, want non-nil empty map", snapshots)
	}

	configured, err := tracker.Get("github")
	if err != nil {
		t.Fatalf("Get(github) error = %v", err)
	}
	if configured != (ServiceSnapshot{ServiceID: "github", Status: StatusUnknown}) {
		t.Fatalf("Get(github) = %+v, want configured UNKNOWN view", configured)
	}

	if _, err := tracker.Get("mutated"); !errors.Is(err, ErrUnknownService) {
		t.Fatalf("Get(mutated) error = %v, want ErrUnknownService", err)
	}
}

func TestNewTrackerValidation(t *testing.T) {
	tests := []struct {
		name       string
		serviceIDs []string
		wantErr    error
	}{
		{
			name:    "zero configured services",
			wantErr: ErrNoEnabledServices,
		},
		{
			name:       "empty service ID",
			serviceIDs: []string{""},
			wantErr:    ErrInvalidServiceID,
		},
		{
			name:       "duplicate service ID",
			serviceIDs: []string{"github", "github"},
			wantErr:    ErrDuplicateServiceID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker, err := NewTracker(tt.serviceIDs)
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

func TestTrackerSnapshotReturnsObservedValueCopies(t *testing.T) {
	tracker := newTestTracker(t, "github", "cloudflare", "discord")
	githubAt := testBaseTime.Add(time.Second)
	cloudflareAt := testBaseTime.Add(2 * time.Second)
	applyObservation(t, tracker, "github", monitoring.ObservationHealthy, githubAt)
	applyObservation(t, tracker, "cloudflare", monitoring.ObservationFailure, cloudflareAt)

	snapshot := tracker.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("Snapshot() length = %d, want 2", len(snapshot))
	}
	if got := snapshot["github"]; got != (SnapshotEntry{State: StatusUp, LastCheckedAt: githubAt}) {
		t.Fatalf("Snapshot()[github] = %+v, want UP at %s", got, githubAt)
	}
	if got := snapshot["cloudflare"]; got != (SnapshotEntry{State: StatusUnknown, LastCheckedAt: cloudflareAt}) {
		t.Fatalf("Snapshot()[cloudflare] = %+v, want UNKNOWN at %s", got, cloudflareAt)
	}
	if _, exists := snapshot["discord"]; exists {
		t.Fatal("Snapshot() contains configured but unobserved discord")
	}
	for serviceID, entry := range snapshot {
		if entry.LastCheckedAt.IsZero() {
			t.Fatalf("Snapshot()[%q].LastCheckedAt is zero", serviceID)
		}
	}

	delete(snapshot, "github")
	snapshot["cloudflare"] = SnapshotEntry{State: StatusDown, LastCheckedAt: testBaseTime}
	snapshot["fake"] = SnapshotEntry{State: StatusDown, LastCheckedAt: testBaseTime}
	next := tracker.Snapshot()
	if len(next) != 2 || next["github"] != (SnapshotEntry{State: StatusUp, LastCheckedAt: githubAt}) ||
		next["cloudflare"] != (SnapshotEntry{State: StatusUnknown, LastCheckedAt: cloudflareAt}) {
		t.Fatalf("Snapshot() exposed mutable state: got %+v", next)
	}
	if _, exists := next["fake"]; exists {
		t.Fatal("Snapshot() retained caller-added service")
	}

	got, err := tracker.Get("github")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ServiceID != "github" || got.Status != StatusUp || got.LastObservedAt != githubAt {
		t.Fatalf("Get() snapshot = %+v, want github up at %s", got, githubAt)
	}
}

func TestTrackerSnapshotRemainsIndependentAfterNewObservation(t *testing.T) {
	tracker := newTestTracker(t, "github")
	firstAt := testBaseTime.Add(time.Second)
	secondAt := testBaseTime.Add(2 * time.Second)
	applyObservation(t, tracker, "github", monitoring.ObservationHealthy, firstAt)

	first := tracker.Snapshot()
	applyObservation(t, tracker, "github", monitoring.ObservationHealthy, secondAt)
	second := tracker.Snapshot()

	if got := first["github"]; got != (SnapshotEntry{State: StatusUp, LastCheckedAt: firstAt}) {
		t.Fatalf("first Snapshot()[github] = %+v, want UP at %s", got, firstAt)
	}
	if got := second["github"]; got != (SnapshotEntry{State: StatusUp, LastCheckedAt: secondAt}) {
		t.Fatalf("second Snapshot()[github] = %+v, want UP at %s", got, secondAt)
	}
}

func TestTrackerSnapshotTimestampFollowsAcceptedObservations(t *testing.T) {
	tracker := newTestTracker(t, "github")
	times := sequentialTimes(5, time.Second)

	applyObservation(t, tracker, "github", monitoring.ObservationHealthy, times[0])
	assertSnapshotEntry(t, tracker, "github", SnapshotEntry{State: StatusUp, LastCheckedAt: times[0]})

	applyObservation(t, tracker, "github", monitoring.ObservationHealthy, times[1])
	assertSnapshotEntry(t, tracker, "github", SnapshotEntry{State: StatusUp, LastCheckedAt: times[1]})

	applyObservation(t, tracker, "github", monitoring.ObservationFailure, times[2])
	assertSnapshotEntry(t, tracker, "github", SnapshotEntry{State: StatusUp, LastCheckedAt: times[2]})
	applyObservation(t, tracker, "github", monitoring.ObservationFailure, times[3])
	applyObservation(t, tracker, "github", monitoring.ObservationFailure, times[4])
	assertSnapshotEntry(t, tracker, "github", SnapshotEntry{State: StatusDown, LastCheckedAt: times[4]})
}

func TestTrackerSnapshotExcludesUnappliedObservations(t *testing.T) {
	tracker := newTestTracker(t, "github")
	ignoredAt := testBaseTime.Add(time.Second)
	applyObservation(t, tracker, "github", monitoring.ObservationIgnored, ignoredAt)
	if snapshot := tracker.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("Snapshot() after first ignored observation = %+v, want empty", snapshot)
	}

	acceptedAt := testBaseTime.Add(3 * time.Second)
	applyObservation(t, tracker, "github", monitoring.ObservationFailure, acceptedAt)
	want := SnapshotEntry{State: StatusUnknown, LastCheckedAt: acceptedAt}
	for _, observation := range []monitoring.Observation{
		newObservation("github", monitoring.ObservationHealthy, acceptedAt.Add(-time.Second)),
		newObservation("github", monitoring.ObservationIndeterminate, acceptedAt),
		newObservation("github", monitoring.ObservationIgnored, acceptedAt.Add(time.Second)),
	} {
		if update, err := tracker.Apply(observation); err != nil || update.Applied {
			t.Fatalf("Apply(%+v) = (%+v, %v), want unapplied observation", observation, update, err)
		}
		assertSnapshotEntry(t, tracker, "github", want)
	}
}

func TestTrackerConcurrentAccessIsRaceSafe(t *testing.T) {
	const serviceCount = 8
	serviceIDs := make([]string, 0, serviceCount)
	for index := range serviceCount {
		serviceIDs = append(serviceIDs, fmt.Sprintf("service-%d", index))
	}

	tracker, err := NewTracker(serviceIDs)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, serviceCount)
	start := make(chan struct{})

	for index, serviceID := range serviceIDs {
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
		}(index, serviceID)
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

func TestTrackerConcurrentSnapshotReaders(t *testing.T) {
	const readerCount = 8
	tracker := newTestTracker(t, "github")
	observedAt := testBaseTime.Add(time.Second)
	applyObservation(t, tracker, "github", monitoring.ObservationHealthy, observedAt)
	want := SnapshotEntry{State: StatusUp, LastCheckedAt: observedAt}

	start := make(chan struct{})
	errs := make(chan error, readerCount)
	var wg sync.WaitGroup
	for range readerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 100 {
				if got := tracker.Snapshot()["github"]; got != want {
					errs <- fmt.Errorf("Snapshot()[github] = %+v, want %+v", got, want)
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
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

	tracker, err := NewTracker(serviceIDs)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}

	return tracker
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

func assertSnapshotEntry(t *testing.T, tracker *Tracker, serviceID string, want SnapshotEntry) {
	t.Helper()

	got, exists := tracker.Snapshot()[serviceID]
	if !exists || got != want {
		t.Fatalf("Snapshot()[%q] = (%+v, %t), want %+v", serviceID, got, exists, want)
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

func equalSnapshots(a, b Snapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for serviceID, entry := range a {
		if other, exists := b[serviceID]; !exists || other != entry {
			return false
		}
	}
	return true
}
