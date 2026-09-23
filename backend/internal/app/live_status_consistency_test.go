package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/event"
	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
	"github.com/jowongx8/backend/internal/storage/sqlite"
)

const resultsClosedWhileActive = "results channel closed while monitoring active"

func TestLiveStatusConsistencyAcrossDownAndRecovery(t *testing.T) {
	harness := newLiveStatusConsistencyHarness(t)
	base := time.Date(2026, 9, 24, 10, 0, 0, 123_456_789, time.UTC)

	harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: base, StatusCode: 200})
	initial := harness.takeEvents(1)
	if _, ok := initial[0].(event.StatusChanged); !ok {
		t.Fatalf("initial event = %T, want StatusChanged", initial[0])
	}

	statusBeforeDown, incidentsBeforeDown := harness.snapshots()
	for index := 1; index <= 2; index++ {
		harness.process(monitoring.CheckResult{
			ServiceID:  harness.service.ID,
			CheckedAt:  base.Add(time.Duration(index) * time.Second),
			StatusCode: 503,
		})
		harness.assertNoEvents()
	}
	downAt := base.Add(3 * time.Second)
	harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: downAt, StatusCode: 503})
	downEvents := harness.takeEvents(2)
	opened := assertDownEvents(t, downEvents, harness.service.ID, downAt)
	statusAfterDown, incidentsAfterDown := harness.snapshots()
	openedKey := incidentKey(opened.ServiceID, publicTimestamp(t, opened.StartedAt))

	for _, test := range []struct {
		name      string
		statuses  []ServiceStatusView
		incidents IncidentsView
	}{
		{name: "both snapshots before", statuses: statusBeforeDown, incidents: incidentsBeforeDown},
		{name: "status before and incidents after", statuses: statusBeforeDown, incidents: incidentsAfterDown},
		{name: "incidents before and status after", statuses: statusAfterDown, incidents: incidentsBeforeDown},
		{name: "both snapshots after", statuses: statusAfterDown, incidents: incidentsAfterDown},
	} {
		t.Run("down/"+test.name, func(t *testing.T) {
			model := newReconciliationModel(t, test.statuses, test.incidents)
			model.applyDomainEvents(t, downEvents)
			assertReconciledStatus(t, model, harness.service.ID, status.StatusDown, downAt)
			assertIncidentKeys(t, model, []string{openedKey}, nil)

			model.applyDomainEvents(t, downEvents)
			assertIncidentKeys(t, model, []string{openedKey}, nil)
		})
	}

	newerDownAt := base.Add(4 * time.Second)
	harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: newerDownAt, StatusCode: 503})
	harness.assertNoEvents()
	newerDownStatus, _ := harness.snapshots()
	model := newReconciliationModel(t, newerDownStatus, incidentsAfterDown)
	model.applyDomainEvents(t, downEvents)
	assertReconciledStatus(t, model, harness.service.ID, status.StatusDown, newerDownAt)

	firstRecoveryAt := base.Add(5 * time.Second)
	harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: firstRecoveryAt, StatusCode: 200})
	harness.assertNoEvents()
	statusBeforeRecovery, incidentsBeforeRecovery := harness.snapshots()

	recoveredAt := base.Add(6 * time.Second)
	harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: recoveredAt, StatusCode: 200})
	recoveryEvents := harness.takeEvents(2)
	resolved := assertRecoveryEvents(t, recoveryEvents, harness.service.ID, opened.StartedAt, recoveredAt)
	statusAfterRecovery, incidentsAfterRecovery := harness.snapshots()

	if resolved.ResolvedAt != time.UnixMicro(recoveredAt.UnixMicro()).UTC() {
		t.Fatalf("resolved event time = %s, want canonical %s", resolved.ResolvedAt, time.UnixMicro(recoveredAt.UnixMicro()).UTC())
	}
	if len(incidentsAfterRecovery.Resolved) != 1 || incidentsAfterRecovery.Resolved[0].ResolvedAt == nil ||
		*incidentsAfterRecovery.Resolved[0].ResolvedAt != resolved.ResolvedAt {
		t.Fatalf("resolved REST view = %+v, want event resolution %s", incidentsAfterRecovery.Resolved, resolved.ResolvedAt)
	}

	for _, test := range []struct {
		name      string
		statuses  []ServiceStatusView
		incidents IncidentsView
	}{
		{name: "both snapshots before", statuses: statusBeforeRecovery, incidents: incidentsBeforeRecovery},
		{name: "status before and incidents after", statuses: statusBeforeRecovery, incidents: incidentsAfterRecovery},
		{name: "incidents before and status after", statuses: statusAfterRecovery, incidents: incidentsBeforeRecovery},
		{name: "both snapshots after", statuses: statusAfterRecovery, incidents: incidentsAfterRecovery},
	} {
		t.Run("recovery/"+test.name, func(t *testing.T) {
			model := newReconciliationModel(t, test.statuses, test.incidents)
			model.applyDomainEvents(t, recoveryEvents)
			assertReconciledStatus(t, model, harness.service.ID, status.StatusUp, recoveredAt)
			assertIncidentKeys(t, model, nil, []string{openedKey})

			model.applyDomainEvents(t, recoveryEvents)
			assertIncidentKeys(t, model, nil, []string{openedKey})
		})
	}

	completeCycle := append(append([]event.Event(nil), downEvents...), recoveryEvents...)
	for _, test := range []struct {
		name      string
		statuses  []ServiceStatusView
		incidents IncidentsView
	}{
		{name: "snapshots before cycle", statuses: statusBeforeDown, incidents: incidentsBeforeDown},
		{name: "snapshots after cycle", statuses: statusAfterRecovery, incidents: incidentsAfterRecovery},
	} {
		t.Run("complete buffered cycle/"+test.name, func(t *testing.T) {
			model := newReconciliationModel(t, test.statuses, test.incidents)
			model.applyDomainEvents(t, completeCycle)
			assertReconciledStatus(t, model, harness.service.ID, status.StatusUp, recoveredAt)
			assertIncidentKeys(t, model, nil, []string{openedKey})
		})
	}
}

func TestStatusReconciliationRequiresStrictlyNewerEvent(t *testing.T) {
	base := time.Date(2026, 9, 24, 11, 0, 0, 0, time.UTC)
	model := newReconciliationModel(t, []ServiceStatusView{{
		ID: "github",
		Status: CurrentStatusView{
			State:         status.StatusUp,
			LastCheckedAt: timePointer(base),
		},
	}}, IncidentsView{})

	for _, value := range []reconciliationEvent{
		statusWireEvent("github", status.StatusDown, base.Add(-time.Second)),
		statusWireEvent("github", status.StatusDown, base),
	} {
		model.apply(value)
		assertReconciledStatus(t, model, "github", status.StatusUp, base)
	}

	newer := base.Add(time.Second)
	model.apply(statusWireEvent("github", status.StatusDown, newer))
	assertReconciledStatus(t, model, "github", status.StatusDown, newer)

	model.apply(statusWireEvent("github", status.StatusUp, newer))
	model.apply(statusWireEvent("github", status.StatusUp, base))
	assertReconciledStatus(t, model, "github", status.StatusDown, newer)

	withoutTimestamp := newReconciliationModel(t, []ServiceStatusView{{
		ID:     "new-service",
		Status: CurrentStatusView{State: status.StatusUnknown},
	}}, IncidentsView{})
	withoutTimestamp.apply(statusWireEvent("new-service", status.StatusUp, base))
	assertReconciledStatus(t, withoutTimestamp, "new-service", status.StatusUp, base)
}

func TestIncidentReconciliationIsIdempotent(t *testing.T) {
	startedAt := "2026-09-24T12:00:00.123456+00:00"
	resolvedAt := "2026-09-24T12:05:00.654321+00:00"
	key := incidentKey("github", startedAt)
	opened := incidentWireEvent("incident.opened", "github", startedAt, "")
	resolved := incidentWireEvent("incident.resolved", "github", startedAt, resolvedAt)

	t.Run("duplicate opening remains one active incident", func(t *testing.T) {
		model := newReconciliationModel(t, nil, IncidentsView{})
		model.apply(opened)
		model.apply(opened)
		assertIncidentKeys(t, model, []string{key}, nil)
		if model.active[key].startedAt != startedAt {
			t.Fatalf("stored identity timestamp = %q, want raw %q", model.active[key].startedAt, startedAt)
		}
	})

	t.Run("resolution moves active incident once", func(t *testing.T) {
		model := newReconciliationModel(t, nil, IncidentsView{})
		model.apply(opened)
		model.apply(resolved)
		model.apply(resolved)
		model.apply(opened)
		assertIncidentKeys(t, model, nil, []string{key})
	})

	t.Run("opening already represented as resolved is ignored", func(t *testing.T) {
		model := newReconciliationModel(t, nil, IncidentsView{})
		model.apply(opened)
		model.apply(resolved)
		model.apply(opened)
		assertIncidentKeys(t, model, nil, []string{key})
	})

	t.Run("missing bounded-history resolution is ignored", func(t *testing.T) {
		model := newReconciliationModel(t, nil, IncidentsView{})
		model.apply(resolved)
		assertIncidentKeys(t, model, nil, nil)
	})
}

func TestIncidentReconciliationKeepsBoundedFeed(t *testing.T) {
	t.Run("all active incidents converge when resolved capacity is zero", func(t *testing.T) {
		store, query := newConsistencyIncidentStore(t)
		base := time.Date(2026, 9, 24, 13, 0, 0, 0, time.UTC)
		opened := make([]incident.Incident, 0, publicIncidentTarget+1)
		for index := range publicIncidentTarget + 1 {
			value := incident.Incident{
				ServiceID: fmt.Sprintf("active-%02d", index),
				StartedAt: base.Add(time.Duration(index) * time.Microsecond),
			}
			if err := store.Open(context.Background(), value); err != nil {
				t.Fatal(err)
			}
			opened = append(opened, value)
		}

		before := executeIncidentsQuery(t, query)
		if len(before.Active) != publicIncidentTarget+1 || len(before.Resolved) != 0 {
			t.Fatalf("before feed counts = (%d active, %d resolved)", len(before.Active), len(before.Resolved))
		}

		resolution := opened[0]
		resolvedAt := base.Add(time.Hour)
		resolution.ResolvedAt = &resolvedAt
		if err := store.Resolve(context.Background(), resolution); err != nil {
			t.Fatal(err)
		}
		after := executeIncidentsQuery(t, query)
		if len(after.Active) != publicIncidentTarget || len(after.Resolved) != 0 {
			t.Fatalf("after feed counts = (%d active, %d resolved), want (%d, 0)", len(after.Active), len(after.Resolved), publicIncidentTarget)
		}

		resolvedEvent := domainEventToWire(t, event.IncidentResolved{
			ServiceID:  resolution.ServiceID,
			StartedAt:  resolution.StartedAt,
			ResolvedAt: resolvedAt,
		})
		model := newReconciliationModel(t, nil, before)
		model.apply(resolvedEvent)
		assertIncidentModelMatchesView(t, model, after)

		alreadyReflected := newReconciliationModel(t, nil, after)
		alreadyReflected.apply(resolvedEvent)
		assertIncidentModelMatchesView(t, alreadyReflected, after)

		fullPair := newReconciliationModel(t, nil, after)
		fullPair.apply(domainEventToWire(t, event.IncidentOpened{
			ServiceID: resolution.ServiceID,
			StartedAt: resolution.StartedAt,
		}))
		fullPair.apply(resolvedEvent)
		assertIncidentModelMatchesView(t, fullPair, after)
	})

	t.Run("distinct resolution times retain newest twenty in order", func(t *testing.T) {
		store, query := newConsistencyIncidentStore(t)
		model := newReconciliationModel(t, nil, IncidentsView{})
		base := time.Date(2026, 9, 24, 14, 0, 0, 0, time.UTC)

		for index := range publicIncidentTarget + 2 {
			opened := incident.Incident{
				ServiceID: fmt.Sprintf("resolved-%02d", index),
				StartedAt: base.Add(time.Duration(index) * time.Minute),
			}
			if err := store.Open(context.Background(), opened); err != nil {
				t.Fatal(err)
			}
			resolvedAt := base.Add(24*time.Hour + time.Duration(index)*time.Second)
			resolved := opened
			resolved.ResolvedAt = &resolvedAt
			if err := store.Resolve(context.Background(), resolved); err != nil {
				t.Fatal(err)
			}

			model.apply(domainEventToWire(t, event.IncidentOpened{ServiceID: opened.ServiceID, StartedAt: opened.StartedAt}))
			model.apply(domainEventToWire(t, event.IncidentResolved{
				ServiceID:  opened.ServiceID,
				StartedAt:  opened.StartedAt,
				ResolvedAt: resolvedAt,
			}))
		}

		view := executeIncidentsQuery(t, query)
		if len(view.Active) != 0 || len(view.Resolved) != publicIncidentTarget {
			t.Fatalf("feed counts = (%d active, %d resolved), want (0, %d)", len(view.Active), len(view.Resolved), publicIncidentTarget)
		}
		assertIncidentModelMatchesView(t, model, view)
		values := model.resolvedValues()
		for index := 1; index < len(values); index++ {
			if !values[index-1].resolvedInstant.After(values[index].resolvedInstant) {
				t.Fatalf("resolved order at %d = (%s, %s), want strictly descending", index, values[index-1].resolvedAt, values[index].resolvedAt)
			}
		}
	})
}

func TestReconnectReconciliationUsesFreshSnapshotsWithoutReplay(t *testing.T) {
	harness := newLiveStatusConsistencyHarness(t)
	base := time.Date(2026, 9, 24, 15, 0, 0, 987_654_321, time.UTC)

	harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: base, StatusCode: 200})
	harness.takeEvents(1)
	for index := 1; index <= 3; index++ {
		harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: base.Add(time.Duration(index) * time.Second), StatusCode: 503})
	}
	harness.takeEvents(2)
	staleStatus, staleIncidents := harness.snapshots()
	stale := newReconciliationModel(t, staleStatus, staleIncidents)
	assertReconciledStatus(t, stale, harness.service.ID, status.StatusDown, base.Add(3*time.Second))

	harness.subscription.Close()
	for index := 4; index <= 5; index++ {
		harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: base.Add(time.Duration(index) * time.Second), StatusCode: 200})
	}

	newSubscription, err := harness.broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(newSubscription.Close)
	harness.subscription = newSubscription
	harness.assertNoEvents()

	freshStatus, freshIncidents := harness.snapshots()
	stale.installStatus(freshStatus)
	stale.installIncidents(t, freshIncidents)
	assertReconciledStatus(t, stale, harness.service.ID, status.StatusUp, base.Add(5*time.Second))
	if len(stale.active) != 0 || len(stale.resolved) != 1 {
		t.Fatalf("fresh incident state = (%d active, %d resolved), want (0, 1)", len(stale.active), len(stale.resolved))
	}

	for index := 6; index <= 7; index++ {
		harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: base.Add(time.Duration(index) * time.Second), StatusCode: 503})
		harness.assertNoEvents()
	}
	statusDuringReconnect, incidentsDuringReconnect := harness.snapshots()
	downAgainAt := base.Add(8 * time.Second)
	harness.process(monitoring.CheckResult{ServiceID: harness.service.ID, CheckedAt: downAgainAt, StatusCode: 503})
	newEvents := harness.takeEvents(2)

	stale.installStatus(statusDuringReconnect)
	stale.installIncidents(t, incidentsDuringReconnect)
	stale.applyDomainEvents(t, newEvents)
	assertReconciledStatus(t, stale, harness.service.ID, status.StatusDown, downAgainAt)
	if len(stale.active) != 1 || len(stale.resolved) != 1 {
		t.Fatalf("reconnected incident state = (%d active, %d resolved), want (1, 1)", len(stale.active), len(stale.resolved))
	}
}

type liveStatusConsistencyHarness struct {
	t             *testing.T
	service       service.Service
	tracker       *status.Tracker
	checkResults  *sqlite.CheckResultStore
	processor     *IncidentProcessor
	statusQuery   *CurrentStatusQuery
	incidentQuery *IncidentsQuery
	broker        *event.Broker
	subscription  *event.Subscription
}

func newLiveStatusConsistencyHarness(t *testing.T) *liveStatusConsistencyHarness {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "consistency.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close consistency database: %v", err)
		}
	})

	svc := service.Service{
		ID:         "github",
		Name:       "GitHub",
		Category:   service.CategoryDeveloperCloud,
		WebsiteURL: "https://github.com",
		CheckURL:   "https://github.com",
		Weight:     10,
		Enabled:    true,
	}
	tracker, err := status.NewTracker([]string{svc.ID})
	if err != nil {
		t.Fatal(err)
	}
	incidentStore := sqlite.NewIncidentStore(db)
	statusQuery, err := NewCurrentStatusQuery([]service.Service{svc}, tracker)
	if err != nil {
		t.Fatal(err)
	}
	incidentQuery, err := NewIncidentsQuery(incidentStore)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := event.NewBroker(event.DefaultSubscriberBufferSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	subscription, err := broker.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(subscription.Close)

	return &liveStatusConsistencyHarness{
		t:             t,
		service:       svc,
		tracker:       tracker,
		checkResults:  sqlite.NewCheckResultStore(db),
		processor:     NewIncidentProcessor(incident.NewLifecycle(), incidentStore),
		statusQuery:   statusQuery,
		incidentQuery: incidentQuery,
		broker:        broker,
		subscription:  subscription,
	}
}

func (h *liveStatusConsistencyHarness) process(result monitoring.CheckResult) {
	h.t.Helper()
	results := make(chan monitoring.CheckResult, 1)
	results <- result
	close(results)
	guard := &outstandingSubmitter{outstanding: map[string]struct{}{result.ServiceID: {}}}

	err := processResults(
		context.Background(),
		results,
		h.checkResults,
		h.tracker,
		h.processor,
		h.broker,
		guard,
	)
	if err == nil || !strings.Contains(err.Error(), resultsClosedWhileActive) {
		h.t.Fatalf("processResults() error = %v, want deterministic closed-results completion", err)
	}
	if _, outstanding := guard.outstanding[result.ServiceID]; outstanding {
		h.t.Fatalf("service %q remains outstanding after successful processing", result.ServiceID)
	}
}

func (h *liveStatusConsistencyHarness) takeEvents(count int) []event.Event {
	h.t.Helper()
	values := make([]event.Event, 0, count)
	for index := 0; index < count; index++ {
		select {
		case value, ok := <-h.subscription.Events():
			if !ok {
				h.t.Fatal("subscription closed while reading published events")
			}
			values = append(values, value)
		default:
			h.t.Fatalf("published event %d of %d is not available", index+1, count)
		}
	}
	return values
}

func (h *liveStatusConsistencyHarness) assertNoEvents() {
	h.t.Helper()
	select {
	case value, ok := <-h.subscription.Events():
		if !ok {
			h.t.Fatal("subscription closed unexpectedly")
		}
		h.t.Fatalf("unexpected event %#v", value)
	default:
	}
}

func (h *liveStatusConsistencyHarness) snapshots() ([]ServiceStatusView, IncidentsView) {
	h.t.Helper()
	incidents, err := h.incidentQuery.Execute(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return h.statusQuery.Execute(), incidents
}

func assertDownEvents(t *testing.T, values []event.Event, serviceID string, downAt time.Time) event.IncidentOpened {
	t.Helper()
	if len(values) != 2 {
		t.Fatalf("DOWN events = %#v, want two events", values)
	}
	changed, ok := values[0].(event.StatusChanged)
	if !ok || changed.ServiceID != serviceID || changed.State != status.StatusDown || changed.LastCheckedAt != downAt {
		t.Fatalf("first DOWN event = %#v, want status change at %s", values[0], downAt)
	}
	opened, ok := values[1].(event.IncidentOpened)
	wantStartedAt := time.UnixMicro(downAt.UnixMicro()).UTC()
	if !ok || opened.ServiceID != serviceID || opened.StartedAt != wantStartedAt {
		t.Fatalf("second DOWN event = %#v, want opening at %s", values[1], wantStartedAt)
	}
	return opened
}

func assertRecoveryEvents(t *testing.T, values []event.Event, serviceID string, startedAt, recoveredAt time.Time) event.IncidentResolved {
	t.Helper()
	if len(values) != 2 {
		t.Fatalf("recovery events = %#v, want two events", values)
	}
	changed, ok := values[0].(event.StatusChanged)
	if !ok || changed.ServiceID != serviceID || changed.State != status.StatusUp || changed.LastCheckedAt != recoveredAt {
		t.Fatalf("first recovery event = %#v, want status change at %s", values[0], recoveredAt)
	}
	resolved, ok := values[1].(event.IncidentResolved)
	if !ok || resolved.ServiceID != serviceID || resolved.StartedAt != startedAt {
		t.Fatalf("second recovery event = %#v, want resolution for start %s", values[1], startedAt)
	}
	return resolved
}

type reconciliationStatus struct {
	state         status.ServiceStatus
	lastCheckedAt *time.Time
}

type reconciliationIncident struct {
	serviceID       string
	startedAt       string
	resolvedAt      string
	resolvedInstant time.Time
}

type reconciliationEvent struct {
	name          string
	serviceID     string
	state         status.ServiceStatus
	lastCheckedAt time.Time
	startedAt     string
	resolvedAt    string
}

type reconciliationModel struct {
	statuses map[string]reconciliationStatus
	active   map[string]reconciliationIncident
	resolved map[string]reconciliationIncident
}

func newReconciliationModel(t *testing.T, statuses []ServiceStatusView, incidents IncidentsView) *reconciliationModel {
	t.Helper()
	model := &reconciliationModel{}
	model.installStatus(statuses)
	model.installIncidents(t, incidents)
	return model
}

func (m *reconciliationModel) installStatus(views []ServiceStatusView) {
	m.statuses = make(map[string]reconciliationStatus, len(views))
	for _, view := range views {
		var checkedAt *time.Time
		if view.Status.LastCheckedAt != nil {
			value := *view.Status.LastCheckedAt
			checkedAt = &value
		}
		m.statuses[view.ID] = reconciliationStatus{state: view.Status.State, lastCheckedAt: checkedAt}
	}
}

func (m *reconciliationModel) installIncidents(t *testing.T, view IncidentsView) {
	t.Helper()
	m.active = make(map[string]reconciliationIncident, len(view.Active))
	m.resolved = make(map[string]reconciliationIncident, len(view.Resolved))
	for _, value := range view.Active {
		startedAt := publicTimestamp(t, value.StartedAt)
		m.active[incidentKey(value.ServiceID, startedAt)] = reconciliationIncident{
			serviceID: value.ServiceID,
			startedAt: startedAt,
		}
	}
	for _, value := range view.Resolved {
		if value.ResolvedAt == nil {
			t.Fatalf("resolved REST incident has no resolution: %+v", value)
		}
		startedAt := publicTimestamp(t, value.StartedAt)
		resolvedAt := publicTimestamp(t, *value.ResolvedAt)
		m.resolved[incidentKey(value.ServiceID, startedAt)] = reconciliationIncident{
			serviceID:       value.ServiceID,
			startedAt:       startedAt,
			resolvedAt:      resolvedAt,
			resolvedInstant: *value.ResolvedAt,
		}
	}
	m.trimResolved()
}

func (m *reconciliationModel) applyDomainEvents(t *testing.T, values []event.Event) {
	t.Helper()
	for _, value := range values {
		m.apply(domainEventToWire(t, value))
	}
}

func (m *reconciliationModel) apply(value reconciliationEvent) {
	switch value.name {
	case "status.changed":
		current, exists := m.statuses[value.serviceID]
		if exists && current.lastCheckedAt != nil && !value.lastCheckedAt.After(*current.lastCheckedAt) {
			return
		}
		checkedAt := value.lastCheckedAt
		m.statuses[value.serviceID] = reconciliationStatus{state: value.state, lastCheckedAt: &checkedAt}
	case "incident.opened":
		key := incidentKey(value.serviceID, value.startedAt)
		if _, exists := m.active[key]; exists {
			return
		}
		if _, exists := m.resolved[key]; exists {
			return
		}
		m.active[key] = reconciliationIncident{serviceID: value.serviceID, startedAt: value.startedAt}
		m.trimResolved()
	case "incident.resolved":
		key := incidentKey(value.serviceID, value.startedAt)
		resolvedAt, err := time.Parse(time.RFC3339Nano, value.resolvedAt)
		if err != nil {
			panic(fmt.Sprintf("invalid test resolution timestamp %q: %v", value.resolvedAt, err))
		}
		resolved := reconciliationIncident{
			serviceID:       value.serviceID,
			startedAt:       value.startedAt,
			resolvedAt:      value.resolvedAt,
			resolvedInstant: resolvedAt,
		}
		if _, exists := m.resolved[key]; exists {
			m.resolved[key] = resolved
			m.trimResolved()
			return
		}
		if _, exists := m.active[key]; !exists {
			return
		}
		delete(m.active, key)
		m.resolved[key] = resolved
		m.trimResolved()
	default:
		panic(fmt.Sprintf("unsupported reconciliation event %q", value.name))
	}
}

func (m *reconciliationModel) trimResolved() {
	capacity := publicIncidentTarget - len(m.active)
	if capacity < 0 {
		capacity = 0
	}
	values := m.resolvedValues()
	if len(values) <= capacity {
		return
	}
	for _, value := range values[capacity:] {
		delete(m.resolved, incidentKey(value.serviceID, value.startedAt))
	}
}

func (m *reconciliationModel) resolvedValues() []reconciliationIncident {
	values := make([]reconciliationIncident, 0, len(m.resolved))
	for _, value := range m.resolved {
		values = append(values, value)
	}
	sort.SliceStable(values, func(i, j int) bool {
		return values[i].resolvedInstant.After(values[j].resolvedInstant)
	})
	return values
}

func domainEventToWire(t *testing.T, value event.Event) reconciliationEvent {
	t.Helper()
	switch value := value.(type) {
	case event.StatusChanged:
		return statusWireEvent(value.ServiceID, value.State, value.LastCheckedAt)
	case event.IncidentOpened:
		return incidentWireEvent("incident.opened", value.ServiceID, publicTimestamp(t, value.StartedAt), "")
	case event.IncidentResolved:
		return incidentWireEvent(
			"incident.resolved",
			value.ServiceID,
			publicTimestamp(t, value.StartedAt),
			publicTimestamp(t, value.ResolvedAt),
		)
	default:
		t.Fatalf("unsupported domain event %T", value)
		return reconciliationEvent{}
	}
}

func statusWireEvent(serviceID string, state status.ServiceStatus, checkedAt time.Time) reconciliationEvent {
	return reconciliationEvent{name: "status.changed", serviceID: serviceID, state: state, lastCheckedAt: checkedAt}
}

func incidentWireEvent(name, serviceID, startedAt, resolvedAt string) reconciliationEvent {
	return reconciliationEvent{name: name, serviceID: serviceID, startedAt: startedAt, resolvedAt: resolvedAt}
}

func publicTimestamp(t *testing.T, value time.Time) string {
	t.Helper()
	encoded, err := value.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func incidentKey(serviceID, rawStartedAt string) string {
	return serviceID + "|" + rawStartedAt
}

func assertReconciledStatus(t *testing.T, model *reconciliationModel, serviceID string, state status.ServiceStatus, checkedAt time.Time) {
	t.Helper()
	value, exists := model.statuses[serviceID]
	if !exists || value.state != state || value.lastCheckedAt == nil || *value.lastCheckedAt != checkedAt {
		t.Fatalf("status[%q] = %+v, want %q at %s", serviceID, value, state, checkedAt)
	}
}

func assertIncidentKeys(t *testing.T, model *reconciliationModel, wantActive, wantResolved []string) {
	t.Helper()
	gotActive := make([]string, 0, len(model.active))
	for key := range model.active {
		gotActive = append(gotActive, key)
	}
	sort.Strings(gotActive)
	sort.Strings(wantActive)
	gotResolved := make([]string, 0, len(model.resolved))
	for key := range model.resolved {
		gotResolved = append(gotResolved, key)
	}
	sort.Strings(gotResolved)
	sort.Strings(wantResolved)
	if !slices.Equal(gotActive, wantActive) || !slices.Equal(gotResolved, wantResolved) {
		t.Fatalf("incident keys = (%v active, %v resolved), want (%v, %v)", gotActive, gotResolved, wantActive, wantResolved)
	}
}

func assertIncidentModelMatchesView(t *testing.T, model *reconciliationModel, view IncidentsView) {
	t.Helper()
	wantActive := make([]string, 0, len(view.Active))
	for _, value := range view.Active {
		wantActive = append(wantActive, incidentKey(value.ServiceID, publicTimestamp(t, value.StartedAt)))
	}
	gotActive := make([]string, 0, len(model.active))
	for key := range model.active {
		gotActive = append(gotActive, key)
	}
	sort.Strings(gotActive)
	sort.Strings(wantActive)

	wantResolved := make([]string, 0, len(view.Resolved))
	for _, value := range view.Resolved {
		wantResolved = append(wantResolved, incidentKey(value.ServiceID, publicTimestamp(t, value.StartedAt)))
	}
	gotResolved := make([]string, 0, len(model.resolved))
	for _, value := range model.resolvedValues() {
		gotResolved = append(gotResolved, incidentKey(value.serviceID, value.startedAt))
	}

	if !slices.Equal(gotActive, wantActive) || !slices.Equal(gotResolved, wantResolved) {
		t.Fatalf("model = (%v active, %v resolved), REST = (%v active, %v resolved)", gotActive, gotResolved, wantActive, wantResolved)
	}
}

func newConsistencyIncidentStore(t *testing.T) (*sqlite.IncidentStore, *IncidentsQuery) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "incidents.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close incident consistency database: %v", err)
		}
	})
	store := sqlite.NewIncidentStore(db)
	query, err := NewIncidentsQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	return store, query
}

func executeIncidentsQuery(t *testing.T, query *IncidentsQuery) IncidentsView {
	t.Helper()
	view, err := query.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func timePointer(value time.Time) *time.Time {
	return &value
}
