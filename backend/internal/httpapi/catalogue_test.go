package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/app"
	"github.com/jowongx8/backend/internal/incident"
	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
	"github.com/jowongx8/backend/internal/storage/sqlite"
)

func TestCatalogueAPIsFilterMembershipWithoutDeletingHistory(t *testing.T) {
	for _, mode := range []string{"approved catalogue", "one disabled", "all disabled", "empty catalogue"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			services := service.Catalogue()
			if mode == "empty catalogue" {
				services = nil
			}
			enabledCount := 0
			for i := range services {
				if mode == "all disabled" || (mode == "one disabled" && services[i].ID == "notion") {
					services[i].Enabled = false
				}
				if services[i].Enabled {
					enabledCount++
				}
			}
			trackerIDs := []string{"openai"}
			excluded := []string{"cloudflare", "slack", "google-drive", "rakuten-ichiba", "nintendo-eshop", "not-configured"}
			if mode == "one disabled" {
				excluded = append(excluded, "notion")
			}
			for _, svc := range services {
				if svc.ID != "openai" {
					trackerIDs = append(trackerIDs, svc.ID)
				}
			}
			// Ghost tracker/history entries model an existing database after catalogue cleanup.
			trackerIDs = append(trackerIDs, "cloudflare", "slack", "google-drive", "rakuten-ichiba", "nintendo-eshop", "not-configured")
			tracker, err := status.NewTracker(trackerIDs)
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 11, 12, 12, 0, 0, 123456000, time.UTC)
			if _, err := tracker.Apply(monitoring.Observation{ServiceID: "openai", ObservedAt: at, Kind: monitoring.ObservationHealthy}); err != nil {
				t.Fatal(err)
			}
			db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalogue.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			store := sqlite.NewIncidentStore(db)
			save := func(id string, started time.Time, resolved *time.Time) {
				t.Helper()
				v := incident.Incident{ServiceID: id, StartedAt: started}
				if err := store.Open(ctx, v); err != nil {
					t.Fatal(err)
				}
				if resolved != nil {
					v.ResolvedAt = resolved
					if err := store.Resolve(ctx, v); err != nil {
						t.Fatal(err)
					}
				}
			}
			// More recent excluded history must not consume the public resolved-feed limit.
			for i := range 25 {
				start := at.Add(time.Duration(i+1) * time.Second)
				end := start.Add(time.Second)
				save("cloudflare", start, &end)
			}
			rakutenStart, rakutenEnd := at.Add(-time.Hour), at.Add(time.Second)
			save("rakuten-ichiba", rakutenStart, &rakutenEnd)
			save("nintendo-eshop", rakutenStart, &rakutenEnd)
			if mode == "one disabled" {
				save("notion", rakutenStart, &rakutenEnd)
			}
			for i, id := range excluded {
				save(id, at.Add(time.Duration(i+1)*time.Hour), nil)
			}
			save("github", at, nil)
			started := at.Add(-time.Hour)
			resolved := at.Add(-time.Minute)
			save("openai", started, &resolved)
			var before int
			if err := db.QueryRow("SELECT COUNT(*) FROM incidents").Scan(&before); err != nil {
				t.Fatal(err)
			}
			statuses, err := app.NewCurrentStatusQuery(services, tracker)
			if err != nil {
				t.Fatal(err)
			}
			incidents, err := app.NewIncidentsQuery(store, services)
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(statuses, incidents, unavailableEventSubscriber{})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status API: %d %s", response.Code, response.Body.String())
			}
			var got statusResponse
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Services) != enabledCount {
				t.Fatalf("public catalogue count=%d, want %d catalogue members", len(got.Services), enabledCount)
			}
			index := 0
			for _, want := range services {
				if !want.Enabled {
					continue
				}
				v := got.Services[index]
				index++
				if v.ID != want.ID || v.Name != want.Name || v.Category != string(want.Category) || v.WebsiteURL != want.WebsiteURL || v.Weight != want.Weight {
					t.Fatalf("public metadata/order=%+v, want %+v", v, want)
				}
				if v.ID == "openai" && (v.Name != "ChatGPT" || v.Status.State != "up" || v.Status.LastCheckedAt == nil || !v.Status.LastCheckedAt.Equal(at)) {
					t.Fatalf("canonical ChatGPT identity/status lost: %+v", v)
				}
			}
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("incident API: %d %s", response.Code, response.Body.String())
			}
			var feed incidentsResponse
			if err := json.Unmarshal(response.Body.Bytes(), &feed); err != nil {
				t.Fatal(err)
			}
			if mode == "empty catalogue" || mode == "all disabled" {
				if feed.Active == nil || feed.Resolved == nil || len(feed.Active) != 0 || len(feed.Resolved) != 0 {
					t.Fatalf("empty catalogue exposed history: %+v", feed)
				}
			} else {
				if len(feed.Active) != 1 || feed.Active[0].ServiceID != "github" || len(feed.Resolved) != 1 || feed.Resolved[0].ServiceID != "openai" {
					t.Fatalf("membership/limit filtering failed: %+v", feed)
				}
				if !feed.Resolved[0].StartedAt.Equal(started) || feed.Resolved[0].ResolvedAt == nil || !feed.Resolved[0].ResolvedAt.Equal(resolved) {
					t.Fatalf("historical ChatGPT identity/timestamps changed: %+v", feed.Resolved[0])
				}
			}
			var after int
			if err := db.QueryRow("SELECT COUNT(*) FROM incidents").Scan(&after); err != nil || after != before {
				t.Fatalf("history count changed %d -> %d, error=%v", before, after, err)
			}
		})
	}
}
