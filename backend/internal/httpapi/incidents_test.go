package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/app"
)

type fakeIncidentsReader struct {
	view  app.IncidentsView
	err   error
	ctx   context.Context
	calls int
}

func (f *fakeIncidentsReader) Execute(ctx context.Context) (app.IncidentsView, error) {
	f.calls++
	f.ctx = ctx
	return f.view, f.err
}

func TestIncidentsReturnsPublicIncidentFeed(t *testing.T) {
	activeNewest := time.Date(2026, 9, 22, 14, 0, 0, 123456789, time.FixedZone("AEST", 10*60*60))
	activeOlder := activeNewest.Add(-time.Hour)
	resolvedNewest := activeNewest.Add(-2 * time.Hour)
	resolvedOlder := activeNewest.Add(-3 * time.Hour)
	reader := &fakeIncidentsReader{view: app.IncidentsView{
		Active: []app.IncidentView{
			{ServiceID: "github", StartedAt: activeNewest},
			{ServiceID: "openai", StartedAt: activeOlder},
		},
		Resolved: []app.IncidentView{
			{ServiceID: "discord", StartedAt: resolvedNewest.Add(-time.Hour), ResolvedAt: &resolvedNewest},
			{ServiceID: "slack", StartedAt: resolvedOlder.Add(-time.Hour), ResolvedAt: &resolvedOlder},
		},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	request = request.WithContext(context.WithValue(request.Context(), incidentsContextKey{}, "request"))
	response := httptest.NewRecorder()

	NewRouter(&fakeCurrentStatusReader{}, reader).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if reader.calls != 1 || reader.ctx.Value(incidentsContextKey{}) != "request" {
		t.Fatalf("Execute calls = %d, context value = %v; want 1, request", reader.calls, reader.ctx.Value(incidentsContextKey{}))
	}

	var got map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	want := map[string]any{
		"active": []any{
			map[string]any{
				"serviceId":  "github",
				"startedAt":  "2026-09-22T14:00:00.123456789+10:00",
				"resolvedAt": nil,
			},
			map[string]any{
				"serviceId":  "openai",
				"startedAt":  "2026-09-22T13:00:00.123456789+10:00",
				"resolvedAt": nil,
			},
		},
		"resolved": []any{
			map[string]any{
				"serviceId":  "discord",
				"startedAt":  "2026-09-22T11:00:00.123456789+10:00",
				"resolvedAt": "2026-09-22T12:00:00.123456789+10:00",
			},
			map[string]any{
				"serviceId":  "slack",
				"startedAt":  "2026-09-22T10:00:00.123456789+10:00",
				"resolvedAt": "2026-09-22T11:00:00.123456789+10:00",
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response body = %#v, want %#v", got, want)
	}
}

func TestIncidentsReturnsEmptyArrays(t *testing.T) {
	reader := &fakeIncidentsReader{view: app.IncidentsView{}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	response := httptest.NewRecorder()

	NewRouter(&fakeCurrentStatusReader{}, reader).ServeHTTP(response, request)

	if got := response.Body.String(); got != `{"active":[],"resolved":[]}` {
		t.Fatalf("response body = %q, want nonnull empty incident arrays", got)
	}
	if reader.calls != 1 {
		t.Fatalf("Execute calls = %d, want 1", reader.calls)
	}
}

func TestIncidentsReturnsGenericInternalServerError(t *testing.T) {
	internalMessage := "database path /private/secret: SELECT failed"
	reader := &fakeIncidentsReader{err: errors.New(internalMessage)}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	response := httptest.NewRecorder()

	NewRouter(&fakeCurrentStatusReader{}, reader).ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), internalMessage) || response.Body.String() != "Internal Server Error\n" {
		t.Fatalf("response body = %q, want generic error without internal details", response.Body.String())
	}
	if reader.calls != 1 {
		t.Fatalf("Execute calls = %d, want 1", reader.calls)
	}
}

func TestIncidentsRejectsPostWithoutInvokingReader(t *testing.T) {
	reader := &fakeIncidentsReader{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/incidents", nil)
	response := httptest.NewRecorder()

	NewRouter(&fakeCurrentStatusReader{}, reader).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if reader.calls != 0 {
		t.Fatalf("Execute calls = %d, want 0", reader.calls)
	}
}

type incidentsContextKey struct{}
