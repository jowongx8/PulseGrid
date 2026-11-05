package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(currentStatus CurrentStatusReader, incidents IncidentsReader, events EventSubscriber) http.Handler {
	router := chi.NewRouter()
	router.Get("/health", healthHandler)
	router.Get("/api/v1/status", currentStatusHandler(currentStatus))
	router.Get("/api/v1/incidents", incidentsHandler(incidents))
	router.Get("/api/v1/events", eventStreamHandler(events))

	return router
}
