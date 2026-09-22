package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/app"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/status"
)

type fakeCurrentStatusReader struct {
	views []app.ServiceStatusView
	calls int
}

func (f *fakeCurrentStatusReader) Execute() []app.ServiceStatusView {
	f.calls++
	return f.views
}

func TestCurrentStatusReturnsPublicStatusSnapshot(t *testing.T) {
	checkedAt := time.Date(2026, 9, 21, 14, 51, 58, 123456789, time.FixedZone("AEST", 10*60*60))
	downAt := checkedAt.Add(time.Second)
	reader := &fakeCurrentStatusReader{views: []app.ServiceStatusView{
		{
			ID:         "github",
			Name:       "GitHub",
			Category:   service.CategoryDeveloperCloud,
			WebsiteURL: "https://github.com",
			Weight:     10,
			Status: app.CurrentStatusView{
				State:         status.StatusUp,
				LastCheckedAt: &checkedAt,
			},
		},
		{
			ID:         "openai",
			Name:       "OpenAI",
			Category:   service.CategoryAI,
			WebsiteURL: "https://openai.com",
			Weight:     9,
			Status: app.CurrentStatusView{
				State:         status.StatusDown,
				LastCheckedAt: &downAt,
			},
		},
		{
			ID:         "discord",
			Name:       "Discord",
			Category:   service.CategoryCommunication,
			WebsiteURL: "https://discord.com",
			Weight:     8,
			Status:     app.CurrentStatusView{State: status.StatusUnknown},
		},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()

	NewRouter(reader, &fakeIncidentsReader{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if reader.calls != 1 {
		t.Fatalf("Execute() calls = %d, want 1", reader.calls)
	}

	var got map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	want := map[string]any{
		"services": []any{
			map[string]any{
				"id":         "github",
				"name":       "GitHub",
				"category":   "developer-cloud",
				"websiteUrl": "https://github.com",
				"weight":     float64(10),
				"status": map[string]any{
					"state":         "up",
					"lastCheckedAt": "2026-09-21T14:51:58.123456789+10:00",
				},
			},
			map[string]any{
				"id":         "openai",
				"name":       "OpenAI",
				"category":   "ai",
				"websiteUrl": "https://openai.com",
				"weight":     float64(9),
				"status": map[string]any{
					"state":         "down",
					"lastCheckedAt": "2026-09-21T14:51:59.123456789+10:00",
				},
			},
			map[string]any{
				"id":         "discord",
				"name":       "Discord",
				"category":   "communication",
				"websiteUrl": "https://discord.com",
				"weight":     float64(8),
				"status": map[string]any{
					"state":         "unknown",
					"lastCheckedAt": nil,
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response body = %#v, want %#v", got, want)
	}
}

func TestCurrentStatusReturnsEmptyArray(t *testing.T) {
	reader := &fakeCurrentStatusReader{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()

	NewRouter(reader, &fakeIncidentsReader{}).ServeHTTP(response, request)

	if got := response.Body.String(); got != `{"services":[]}` {
		t.Fatalf("response body = %q, want nonnull empty services array", got)
	}
	if reader.calls != 1 {
		t.Fatalf("Execute() calls = %d, want 1", reader.calls)
	}
}

func TestCurrentStatusRejectsPostWithoutInvokingReader(t *testing.T) {
	reader := &fakeCurrentStatusReader{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/status", nil)
	response := httptest.NewRecorder()

	NewRouter(reader, &fakeIncidentsReader{}).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if reader.calls != 0 {
		t.Fatalf("Execute() calls = %d, want 0", reader.calls)
	}
}
