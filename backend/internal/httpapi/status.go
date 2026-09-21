package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jowongx8/backend/internal/app"
)

type statusResponse struct {
	Services []serviceStatusResponse `json:"services"`
}

type serviceStatusResponse struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Category   string                `json:"category"`
	WebsiteURL string                `json:"websiteUrl"`
	Weight     int                   `json:"weight"`
	Status     currentStatusResponse `json:"status"`
}

type currentStatusResponse struct {
	State         string     `json:"state"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
}

type CurrentStatusReader interface {
	Execute() []app.ServiceStatusView
}

func currentStatusHandler(reader CurrentStatusReader) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		response := newStatusResponse(reader.Execute())
		body, err := json.Marshal(response)
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

func newStatusResponse(views []app.ServiceStatusView) statusResponse {
	services := make([]serviceStatusResponse, 0, len(views))
	for _, view := range views {
		services = append(services, serviceStatusResponse{
			ID:         view.ID,
			Name:       view.Name,
			Category:   string(view.Category),
			WebsiteURL: view.WebsiteURL,
			Weight:     view.Weight,
			Status: currentStatusResponse{
				State:         string(view.Status.State),
				LastCheckedAt: view.Status.LastCheckedAt,
			},
		})
	}

	return statusResponse{Services: services}
}
