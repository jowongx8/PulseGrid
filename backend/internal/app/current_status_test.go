package app

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
)

type fakeStatusSnapshotReader struct {
	snapshot status.Snapshot
	calls    atomic.Int64
}

func (f *fakeStatusSnapshotReader) Snapshot() status.Snapshot {
	f.calls.Add(1)
	return f.snapshot
}

func TestNewCurrentStatusQueryRequiresSnapshotReader(t *testing.T) {
	query, err := NewCurrentStatusQuery(nil, nil)
	if err == nil {
		t.Fatal("NewCurrentStatusQuery() error = nil, want required snapshot reader error")
	}
	if query != nil {
		t.Fatalf("NewCurrentStatusQuery() query = %#v, want nil", query)
	}
}

func TestCurrentStatusQueryReturnsAllUnobservedServicesInConfigurationOrder(t *testing.T) {
	reader := &fakeStatusSnapshotReader{snapshot: status.Snapshot{}}
	services := []service.Service{
		currentStatusService("github", "GitHub", service.CategoryDeveloperCloud, "https://github.com", 10, true),
		currentStatusService("openai", "OpenAI", service.CategoryAI, "https://openai.com", 9, true),
		currentStatusService("discord", "Discord", service.CategoryCommunication, "https://discord.com", 8, true),
	}
	query := newCurrentStatusQuery(t, services, reader)

	got := query.Execute()
	want := []ServiceStatusView{
		{
			ID:         "github",
			Name:       "GitHub",
			Category:   service.CategoryDeveloperCloud,
			WebsiteURL: "https://github.com",
			Weight:     10,
			Status:     CurrentStatusView{State: status.StatusUnknown},
		},
		{
			ID:         "openai",
			Name:       "OpenAI",
			Category:   service.CategoryAI,
			WebsiteURL: "https://openai.com",
			Weight:     9,
			Status:     CurrentStatusView{State: status.StatusUnknown},
		},
		{
			ID:         "discord",
			Name:       "Discord",
			Category:   service.CategoryCommunication,
			WebsiteURL: "https://discord.com",
			Weight:     8,
			Status:     CurrentStatusView{State: status.StatusUnknown},
		},
	}

	if got == nil {
		t.Fatal("Execute() returned nil slice")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Execute() = %+v, want %+v", got, want)
	}
}

func TestCurrentStatusQueryComposesMixedObservedStates(t *testing.T) {
	upAt := time.Date(2026, 9, 21, 10, 0, 1, 0, time.UTC)
	unknownAt := upAt.Add(time.Second)
	downAt := unknownAt.Add(time.Second)
	reader := &fakeStatusSnapshotReader{snapshot: status.Snapshot{
		"other":   {State: status.StatusDown, LastCheckedAt: downAt.Add(time.Hour)},
		"fourth":  {State: status.StatusDown, LastCheckedAt: downAt},
		"github":  {State: status.StatusUp, LastCheckedAt: upAt},
		"discord": {State: status.StatusUnknown, LastCheckedAt: unknownAt},
	}}
	services := []service.Service{
		currentStatusService("github", "GitHub", service.CategoryDeveloperCloud, "https://github.com", 10, true),
		currentStatusService("openai", "OpenAI", service.CategoryAI, "https://openai.com", 9, true),
		currentStatusService("discord", "Discord", service.CategoryCommunication, "https://discord.com", 8, true),
		currentStatusService("fourth", "Fourth", service.CategoryCommerce, "https://fourth.example", 6, true),
	}
	query := newCurrentStatusQuery(t, services, reader)

	got := query.Execute()
	if len(got) != 4 {
		t.Fatalf("len(Execute()) = %d, want 4", len(got))
	}
	assertCurrentStatusView(t, got[0], "github", status.StatusUp, &upAt)
	assertCurrentStatusView(t, got[1], "openai", status.StatusUnknown, nil)
	assertCurrentStatusView(t, got[2], "discord", status.StatusUnknown, &unknownAt)
	assertCurrentStatusView(t, got[3], "fourth", status.StatusDown, &downAt)

	if got[0].Name != "GitHub" || got[0].Category != service.CategoryDeveloperCloud ||
		got[0].WebsiteURL != "https://github.com" || got[0].Weight != 10 {
		t.Fatalf("GitHub metadata = %+v, want configured values", got[0])
	}
	if got[0].Status.LastCheckedAt == got[2].Status.LastCheckedAt ||
		got[2].Status.LastCheckedAt == got[3].Status.LastCheckedAt {
		t.Fatal("observed services share timestamp pointers")
	}
}

func TestCurrentStatusQueryExcludesDisabledServices(t *testing.T) {
	at := time.Date(2026, 9, 21, 11, 0, 0, 0, time.UTC)
	reader := &fakeStatusSnapshotReader{snapshot: status.Snapshot{
		"disabled-a": {State: status.StatusDown, LastCheckedAt: at},
		"enabled":    {State: status.StatusUp, LastCheckedAt: at},
		"disabled-b": {State: status.StatusUnknown, LastCheckedAt: at},
	}}
	services := []service.Service{
		currentStatusService("disabled-a", "Disabled A", service.CategoryAI, "https://a.example", 1, false),
		currentStatusService("enabled", "Enabled", service.CategoryCommerce, "https://enabled.example", 7, true),
		currentStatusService("disabled-b", "Disabled B", service.CategoryGaming, "https://b.example", 2, false),
	}
	query := newCurrentStatusQuery(t, services, reader)

	got := query.Execute()
	if len(got) != 1 || got[0].ID != "enabled" {
		t.Fatalf("Execute() = %+v, want enabled service only", got)
	}
}

func TestCurrentStatusQueryCallsSnapshotOncePerExecution(t *testing.T) {
	reader := &fakeStatusSnapshotReader{snapshot: status.Snapshot{}}
	query := newCurrentStatusQuery(t, []service.Service{
		currentStatusService("github", "GitHub", service.CategoryDeveloperCloud, "https://github.com", 10, true),
	}, reader)

	query.Execute()
	if got := reader.calls.Load(); got != 1 {
		t.Fatalf("Snapshot() calls after one Execute() = %d, want 1", got)
	}
	query.Execute()
	if got := reader.calls.Load(); got != 2 {
		t.Fatalf("Snapshot() calls after two Execute() calls = %d, want 2", got)
	}
}

func TestCurrentStatusQueryOwnsServiceConfiguration(t *testing.T) {
	reader := &fakeStatusSnapshotReader{snapshot: status.Snapshot{}}
	services := []service.Service{
		currentStatusService("github", "GitHub", service.CategoryDeveloperCloud, "https://github.com", 10, true),
		currentStatusService("disabled", "Disabled", service.CategoryAI, "https://disabled.example", 1, false),
	}
	query := newCurrentStatusQuery(t, services, reader)

	services[0] = currentStatusService("mutated", "Mutated", service.CategoryGaming, "https://mutated.example", 2, true)
	services[1].Enabled = true

	got := query.Execute()
	if len(got) != 1 {
		t.Fatalf("len(Execute()) = %d, want 1", len(got))
	}
	want := ServiceStatusView{
		ID:         "github",
		Name:       "GitHub",
		Category:   service.CategoryDeveloperCloud,
		WebsiteURL: "https://github.com",
		Weight:     10,
		Status:     CurrentStatusView{State: status.StatusUnknown},
	}
	if got[0] != want {
		t.Fatalf("Execute()[0] = %+v, want constructor-time service %+v", got[0], want)
	}
}

func TestCurrentStatusQueryResultsAreIndependent(t *testing.T) {
	checkedAt := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	reader := &fakeStatusSnapshotReader{snapshot: status.Snapshot{
		"github": {State: status.StatusUp, LastCheckedAt: checkedAt},
	}}
	query := newCurrentStatusQuery(t, []service.Service{
		currentStatusService("github", "GitHub", service.CategoryDeveloperCloud, "https://github.com", 10, true),
	}, reader)

	first := query.Execute()
	firstTimestamp := first[0].Status.LastCheckedAt
	first[0].ID = "mutated"
	first[0].Status.State = status.StatusDown
	*first[0].Status.LastCheckedAt = checkedAt.Add(time.Hour)

	second := query.Execute()
	assertCurrentStatusView(t, second[0], "github", status.StatusUp, &checkedAt)
	if second[0].Status.LastCheckedAt == firstTimestamp {
		t.Fatal("separate Execute() results share timestamp pointers")
	}
	if entry := reader.snapshot["github"]; entry.LastCheckedAt != checkedAt {
		t.Fatalf("snapshot timestamp = %s, want unchanged %s", entry.LastCheckedAt, checkedAt)
	}
}

func TestCurrentStatusQueryAcceptsNoEnabledServices(t *testing.T) {
	reader := &fakeStatusSnapshotReader{snapshot: status.Snapshot{}}
	query := newCurrentStatusQuery(t, []service.Service{
		currentStatusService("disabled", "Disabled", service.CategoryAI, "https://disabled.example", 1, false),
	}, reader)

	got := query.Execute()
	if got == nil || len(got) != 0 {
		t.Fatalf("Execute() = %#v, want non-nil empty slice", got)
	}
	if calls := reader.calls.Load(); calls != 1 {
		t.Fatalf("Snapshot() calls = %d, want 1", calls)
	}
}

func TestCurrentStatusQuerySupportsConcurrentExecution(t *testing.T) {
	const callerCount = 16
	checkedAt := time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)
	reader := &fakeStatusSnapshotReader{snapshot: status.Snapshot{
		"github": {State: status.StatusUp, LastCheckedAt: checkedAt},
	}}
	query := newCurrentStatusQuery(t, []service.Service{
		currentStatusService("github", "GitHub", service.CategoryDeveloperCloud, "https://github.com", 10, true),
		currentStatusService("openai", "OpenAI", service.CategoryAI, "https://openai.com", 9, true),
	}, reader)

	start := make(chan struct{})
	errs := make(chan error, callerCount)
	var wg sync.WaitGroup
	for range callerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got := query.Execute()
			if len(got) != 2 || got[0].ID != "github" || got[0].Status.State != status.StatusUp ||
				got[0].Status.LastCheckedAt == nil || *got[0].Status.LastCheckedAt != checkedAt ||
				got[1].ID != "openai" || got[1].Status.State != status.StatusUnknown ||
				got[1].Status.LastCheckedAt != nil {
				errs <- fmt.Errorf("Execute() = %+v, want independent configured views", got)
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if calls := reader.calls.Load(); calls != callerCount {
		t.Fatalf("Snapshot() calls = %d, want %d", calls, callerCount)
	}
}

func newCurrentStatusQuery(t *testing.T, services []service.Service, reader StatusSnapshotReader) *CurrentStatusQuery {
	t.Helper()

	query, err := NewCurrentStatusQuery(services, reader)
	if err != nil {
		t.Fatalf("NewCurrentStatusQuery() error = %v", err)
	}
	return query
}

func currentStatusService(
	id string,
	name string,
	category service.Category,
	websiteURL string,
	weight int,
	enabled bool,
) service.Service {
	return service.Service{
		ID:         id,
		Name:       name,
		Category:   category,
		WebsiteURL: websiteURL,
		CheckURL:   "https://check.example",
		Weight:     weight,
		Enabled:    enabled,
	}
}

func assertCurrentStatusView(
	t *testing.T,
	got ServiceStatusView,
	wantID string,
	wantState status.ServiceStatus,
	wantCheckedAt *time.Time,
) {
	t.Helper()

	if got.ID != wantID || got.Status.State != wantState {
		t.Fatalf("service view = %+v, want ID %q and state %q", got, wantID, wantState)
	}
	if wantCheckedAt == nil {
		if got.Status.LastCheckedAt != nil {
			t.Fatalf("service %q LastCheckedAt = %s, want nil", wantID, *got.Status.LastCheckedAt)
		}
		return
	}
	if got.Status.LastCheckedAt == nil || *got.Status.LastCheckedAt != *wantCheckedAt {
		t.Fatalf("service %q LastCheckedAt = %v, want %s", wantID, got.Status.LastCheckedAt, *wantCheckedAt)
	}
}
