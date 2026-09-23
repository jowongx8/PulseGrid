package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/app"
	"github.com/jowongx8/backend/internal/event"
	"github.com/jowongx8/backend/internal/status"
)

func TestRESTAndSSEConsistencyUsesIdenticalTimestampStrings(t *testing.T) {
	statusAt := time.Date(2026, 9, 24, 16, 0, 0, 123_456_789, time.FixedZone("AEST", 10*60*60))
	activeStartedAt := time.Date(2026, 9, 24, 5, 0, 0, 123_456_000, time.UTC)
	resolvedStartedAt := time.Date(2026, 9, 24, 6, 0, 0, 234_567_000, time.UTC)
	resolvedAt := time.Date(2026, 9, 24, 6, 5, 0, 654_321_000, time.UTC)

	statusBody, err := json.Marshal(newStatusResponse([]app.ServiceStatusView{{
		ID: "github",
		Status: app.CurrentStatusView{
			State:         status.StatusDown,
			LastCheckedAt: &statusAt,
		},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	var restStatus struct {
		Services []struct {
			Status struct {
				LastCheckedAt string `json:"lastCheckedAt"`
			} `json:"status"`
		} `json:"services"`
	}
	if err := json.Unmarshal(statusBody, &restStatus); err != nil {
		t.Fatal(err)
	}
	statusFrame, err := marshalEventFrame(event.StatusChanged{
		ServiceID:     "github",
		State:         status.StatusDown,
		LastCheckedAt: statusAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	var streamStatus struct {
		LastCheckedAt string `json:"lastCheckedAt"`
	}
	if err := json.Unmarshal(eventFrameData(t, statusFrame), &streamStatus); err != nil {
		t.Fatal(err)
	}
	if len(restStatus.Services) != 1 || restStatus.Services[0].Status.LastCheckedAt != streamStatus.LastCheckedAt {
		t.Fatalf("status timestamps = (REST %q, SSE %q), want identical", restStatus.Services[0].Status.LastCheckedAt, streamStatus.LastCheckedAt)
	}

	incidentBody, err := json.Marshal(newIncidentsResponse(app.IncidentsView{
		Active: []app.IncidentView{{
			ServiceID: "github",
			StartedAt: activeStartedAt,
		}},
		Resolved: []app.IncidentView{{
			ServiceID:  "openai",
			StartedAt:  resolvedStartedAt,
			ResolvedAt: &resolvedAt,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	type rawIncident struct {
		ServiceID  string  `json:"serviceId"`
		StartedAt  string  `json:"startedAt"`
		ResolvedAt *string `json:"resolvedAt"`
	}
	var restIncidents struct {
		Active   []rawIncident `json:"active"`
		Resolved []rawIncident `json:"resolved"`
	}
	if err := json.Unmarshal(incidentBody, &restIncidents); err != nil {
		t.Fatal(err)
	}

	openedFrame, err := marshalEventFrame(event.IncidentOpened{ServiceID: "github", StartedAt: activeStartedAt})
	if err != nil {
		t.Fatal(err)
	}
	var streamedOpening rawIncident
	if err := json.Unmarshal(eventFrameData(t, openedFrame), &streamedOpening); err != nil {
		t.Fatal(err)
	}
	resolvedFrame, err := marshalEventFrame(event.IncidentResolved{
		ServiceID:  "openai",
		StartedAt:  resolvedStartedAt,
		ResolvedAt: resolvedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	var streamedResolution rawIncident
	if err := json.Unmarshal(eventFrameData(t, resolvedFrame), &streamedResolution); err != nil {
		t.Fatal(err)
	}

	if len(restIncidents.Active) != 1 || restIncidents.Active[0] != streamedOpening {
		t.Fatalf("opening payloads = (REST %+v, SSE %+v), want identical", restIncidents.Active, streamedOpening)
	}
	if len(restIncidents.Resolved) != 1 || !equalRawIncident(restIncidents.Resolved[0], streamedResolution) {
		t.Fatalf("resolution payloads = (REST %+v, SSE %+v), want identical", restIncidents.Resolved, streamedResolution)
	}
	if got := restIncidents.Active[0].StartedAt; got != "2026-09-24T05:00:00.123456Z" {
		t.Fatalf("active startedAt = %q, want canonical UTC microseconds", got)
	}
	if got := restIncidents.Resolved[0].StartedAt; got != "2026-09-24T06:00:00.234567Z" {
		t.Fatalf("resolved startedAt = %q, want canonical UTC microseconds", got)
	}
	if restIncidents.Resolved[0].ResolvedAt == nil || *restIncidents.Resolved[0].ResolvedAt != "2026-09-24T06:05:00.654321Z" {
		t.Fatalf("resolvedAt = %v, want canonical UTC microseconds", restIncidents.Resolved[0].ResolvedAt)
	}

	restActiveKey := restIncidents.Active[0].ServiceID + "|" + restIncidents.Active[0].StartedAt
	streamActiveKey := streamedOpening.ServiceID + "|" + streamedOpening.StartedAt
	restResolvedKey := restIncidents.Resolved[0].ServiceID + "|" + restIncidents.Resolved[0].StartedAt
	streamResolvedKey := streamedResolution.ServiceID + "|" + streamedResolution.StartedAt
	if restActiveKey != streamActiveKey || restResolvedKey != streamResolvedKey {
		t.Fatalf("incident keys = (REST %q/%q, SSE %q/%q), want exact raw-string matches", restActiveKey, restResolvedKey, streamActiveKey, streamResolvedKey)
	}
}

func eventFrameData(t *testing.T, frame []byte) []byte {
	t.Helper()
	for _, line := range strings.Split(string(frame), "\n") {
		if strings.HasPrefix(line, "data: ") {
			return []byte(strings.TrimPrefix(line, "data: "))
		}
	}
	t.Fatalf("SSE frame has no data line: %q", frame)
	return nil
}

func equalRawIncident(left, right struct {
	ServiceID  string  `json:"serviceId"`
	StartedAt  string  `json:"startedAt"`
	ResolvedAt *string `json:"resolvedAt"`
}) bool {
	if left.ServiceID != right.ServiceID || left.StartedAt != right.StartedAt {
		return false
	}
	if left.ResolvedAt == nil || right.ResolvedAt == nil {
		return left.ResolvedAt == nil && right.ResolvedAt == nil
	}
	return *left.ResolvedAt == *right.ResolvedAt
}
