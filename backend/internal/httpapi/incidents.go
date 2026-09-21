package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jowongx8/backend/internal/app"
)

type incidentsResponse struct {
	Active   []incidentResponse `json:"active"`
	Resolved []incidentResponse `json:"resolved"`
}

type incidentResponse struct {
	ServiceID  string     `json:"serviceId"`
	StartedAt  time.Time  `json:"startedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

type IncidentsReader interface {
	Execute(context.Context) (app.IncidentsView, error)
}

func incidentsHandler(reader IncidentsReader) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		view, err := reader.Execute(request.Context())
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		body, err := json.Marshal(newIncidentsResponse(view))
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

func newIncidentsResponse(view app.IncidentsView) incidentsResponse {
	return incidentsResponse{
		Active:   incidentResponses(view.Active),
		Resolved: incidentResponses(view.Resolved),
	}
}

func incidentResponses(views []app.IncidentView) []incidentResponse {
	responses := make([]incidentResponse, 0, len(views))
	for _, view := range views {
		responses = append(responses, incidentResponse{
			ServiceID:  view.ServiceID,
			StartedAt:  view.StartedAt,
			ResolvedAt: view.ResolvedAt,
		})
	}
	return responses
}
