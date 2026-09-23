package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jowongx8/backend/internal/event"
)

const (
	sseHeartbeatInterval = 30 * time.Second
	heartbeatFrame       = ": keep-alive\n\n"
)

type EventSubscriber interface {
	Subscribe() (*event.Subscription, error)
}

type statusChangedEventResponse struct {
	ServiceID     string    `json:"serviceId"`
	State         string    `json:"state"`
	LastCheckedAt time.Time `json:"lastCheckedAt"`
}

type incidentEventResponse struct {
	ServiceID  string     `json:"serviceId"`
	StartedAt  time.Time  `json:"startedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

func eventStreamHandler(subscriber EventSubscriber) http.HandlerFunc {
	return newEventStreamHandler(subscriber, sseHeartbeatInterval)
}

func newEventStreamHandler(subscriber EventSubscriber, heartbeatInterval time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		subscription, err := subscriber.Subscribe()
		if err != nil {
			if errors.Is(err, event.ErrClosed) {
				http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
				return
			}
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if subscription == nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer subscription.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		controller := http.NewResponseController(w)
		if err := controller.Flush(); err != nil {
			return
		}

		heartbeat := time.NewTicker(heartbeatInterval)
		defer heartbeat.Stop()

		for {
			select {
			case <-request.Context().Done():
				return
			case value, ok := <-subscription.Events():
				if !ok {
					return
				}
				frame, err := marshalEventFrame(value)
				if err != nil {
					return
				}
				if err := writeAndFlush(w, controller, frame); err != nil {
					return
				}
			case <-heartbeat.C:
				if err := writeAndFlush(w, controller, []byte(heartbeatFrame)); err != nil {
					return
				}
			}
		}
	}
}

func marshalEventFrame(value event.Event) ([]byte, error) {
	var name string
	var payload any

	switch value := value.(type) {
	case event.StatusChanged:
		name = "status.changed"
		payload = statusChangedEventResponse{
			ServiceID:     value.ServiceID,
			State:         string(value.State),
			LastCheckedAt: value.LastCheckedAt,
		}
	case event.IncidentOpened:
		name = "incident.opened"
		payload = incidentEventResponse{
			ServiceID: value.ServiceID,
			StartedAt: value.StartedAt,
		}
	case event.IncidentResolved:
		name = "incident.resolved"
		resolvedAt := value.ResolvedAt
		payload = incidentEventResponse{
			ServiceID:  value.ServiceID,
			StartedAt:  value.StartedAt,
			ResolvedAt: &resolvedAt,
		}
	default:
		return nil, fmt.Errorf("unsupported event type %T", value)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s event: %w", name, err)
	}

	var frame bytes.Buffer
	frame.WriteString("event: ")
	frame.WriteString(name)
	frame.WriteByte('\n')
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		frame.WriteString("data: ")
		frame.Write(line)
		frame.WriteByte('\n')
	}
	frame.WriteByte('\n')
	return frame.Bytes(), nil
}

func writeAndFlush(w http.ResponseWriter, controller *http.ResponseController, body []byte) error {
	written, err := w.Write(body)
	if err != nil {
		return err
	}
	if written != len(body) {
		return io.ErrShortWrite
	}
	return controller.Flush()
}
