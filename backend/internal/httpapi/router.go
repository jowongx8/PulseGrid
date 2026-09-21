package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(currentStatus CurrentStatusReader) http.Handler {
	router := chi.NewRouter()
	router.Get("/health", healthHandler)
	router.Get("/api/v1/status", currentStatusHandler(currentStatus))

	return router
}
