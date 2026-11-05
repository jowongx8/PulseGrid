package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/event"
	"github.com/jowongx8/backend/internal/status"
)

const eventHTTPTestTimeout = 5 * time.Second

func TestEventStreamFlushesSuccessfulResponseImmediately(t *testing.T) {
	broker := newHTTPTestBroker(t, event.DefaultSubscriberBufferSize)
	response, _, _ := openEventStream(t, NewRouter(&fakeCurrentStatusReader{}, &fakeIncidentsReader{}, broker))

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if got := response.Header.Get("Connection"); got != "" {
		t.Fatalf("Connection = %q, want empty", got)
	}
}

func TestEventStreamMapsAndOrdersPublicEvents(t *testing.T) {
	broker := newHTTPTestBroker(t, event.DefaultSubscriberBufferSize)
	_, reader, _ := openEventStream(t, NewRouter(&fakeCurrentStatusReader{}, &fakeIncidentsReader{}, broker))
	startedAt := time.Date(2026, 9, 22, 15, 0, 0, 123456789, time.FixedZone("AEST", 10*60*60))
	resolvedAt := startedAt.Add(8 * time.Minute)
	events := []event.Event{
		event.StatusChanged{ServiceID: "github", State: status.StatusDown, LastCheckedAt: startedAt},
		event.IncidentOpened{ServiceID: "github", StartedAt: startedAt},
		event.IncidentResolved{ServiceID: "github", StartedAt: startedAt, ResolvedAt: resolvedAt},
	}
	want := [][]string{
		{
			"event: status.changed",
			`data: {"serviceId":"github","state":"down","lastCheckedAt":"2026-09-22T15:00:00.123456789+10:00"}`,
		},
		{
			"event: incident.opened",
			`data: {"serviceId":"github","startedAt":"2026-09-22T15:00:00.123456789+10:00","resolvedAt":null}`,
		},
		{
			"event: incident.resolved",
			`data: {"serviceId":"github","startedAt":"2026-09-22T15:00:00.123456789+10:00","resolvedAt":"2026-09-22T15:08:00.123456789+10:00"}`,
		},
	}

	for _, value := range events {
		if err := broker.Publish(value); err != nil {
			t.Fatal(err)
		}
	}
	for index := range want {
		if got := readSSERecord(t, reader); !reflect.DeepEqual(got, want[index]) {
			t.Fatalf("record %d = %#v, want %#v", index, got, want[index])
		}
	}
}

func TestEventStreamSendsCommentHeartbeat(t *testing.T) {
	broker := newHTTPTestBroker(t, event.DefaultSubscriberBufferSize)
	if sseHeartbeatInterval != 30*time.Second {
		t.Fatalf("production heartbeat interval = %s, want 30s", sseHeartbeatInterval)
	}
	_, reader, _ := openEventStream(t, newEventStreamHandler(broker, time.Millisecond))

	if got, want := readSSERecord(t, reader), []string{": keep-alive"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("heartbeat = %#v, want %#v", got, want)
	}
}

func TestEventStreamClientCancellationClosesSubscription(t *testing.T) {
	broker := newHTTPTestBroker(t, event.DefaultSubscriberBufferSize)
	subscriber := &recordingEventSubscriber{broker: broker, subscriptions: make(chan *event.Subscription, 1)}
	done := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		eventStreamHandler(subscriber).ServeHTTP(w, request)
		close(done)
	})
	_, _, cancel := openEventStream(t, handler)
	subscription := waitHTTPTestValue(t, subscriber.subscriptions)

	cancel()
	waitHTTPTestSignal(t, done)
	assertEventChannelClosed(t, subscription.Events())
}

func TestEventStreamExitsWhenBrokerCloses(t *testing.T) {
	broker := newHTTPTestBroker(t, event.DefaultSubscriberBufferSize)
	done := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		eventStreamHandler(broker).ServeHTTP(w, request)
		close(done)
	})
	_, _, _ = openEventStream(t, handler)

	broker.Close()
	waitHTTPTestSignal(t, done)
}

func TestEventStreamExitsAfterSlowSubscriptionIsDisconnected(t *testing.T) {
	broker := newHTTPTestBroker(t, 1)
	writer := newBlockingStreamWriter()
	t.Cleanup(writer.release)
	done := make(chan struct{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	go func() {
		newEventStreamHandler(broker, time.Hour).ServeHTTP(writer, request)
		close(done)
	}()
	waitHTTPTestSignal(t, writer.initialFlushed)

	startedAt := time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)
	if err := broker.Publish(event.IncidentOpened{ServiceID: "github", StartedAt: startedAt}); err != nil {
		t.Fatal(err)
	}
	waitHTTPTestSignal(t, writer.writeStarted)
	if err := broker.Publish(event.StatusChanged{ServiceID: "github", State: status.StatusDown, LastCheckedAt: startedAt}); err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(event.IncidentResolved{ServiceID: "github", StartedAt: startedAt, ResolvedAt: startedAt.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}

	writer.release()
	waitHTTPTestSignal(t, done)
}

func TestEventStreamHandlesPreStreamFailures(t *testing.T) {
	unexpectedErr := errors.New("subscribe failed with internal details")

	for _, test := range []struct {
		name       string
		subscriber EventSubscriber
		wantStatus int
	}{
		{name: "closed broker", subscriber: eventSubscriberFunc(func() (*event.Subscription, error) {
			return nil, event.ErrClosed
		}), wantStatus: http.StatusServiceUnavailable},
		{name: "unexpected error", subscriber: eventSubscriberFunc(func() (*event.Subscription, error) {
			return nil, unexpectedErr
		}), wantStatus: http.StatusInternalServerError},
		{name: "nil subscription", subscriber: eventSubscriberFunc(func() (*event.Subscription, error) {
			return nil, nil
		}), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)

			eventStreamHandler(test.subscriber).ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.Contains(response.Body.String(), unexpectedErr.Error()) {
				t.Fatalf("response leaked internal error: %q", response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got == "text/event-stream" {
				t.Fatalf("Content-Type = %q before stream establishment", got)
			}
		})
	}
}

func TestEventStreamRequiresFlushingBeforeSubscribing(t *testing.T) {
	calls := 0
	subscriber := eventSubscriberFunc(func() (*event.Subscription, error) {
		calls++
		return nil, errors.New("must not be called")
	})
	response := newNonFlushingResponseWriter()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)

	eventStreamHandler(subscriber).ServeHTTP(response, request)

	if response.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.status, http.StatusInternalServerError)
	}
	if calls != 0 {
		t.Fatalf("Subscribe calls = %d, want 0", calls)
	}
	if got := response.header.Get("Content-Type"); got == "text/event-stream" {
		t.Fatalf("Content-Type = %q before flush support check", got)
	}
}

func TestEventStreamTerminatesOnPostHeaderWriteOrFlushFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		writer func() controlledStreamWriter
	}{
		{name: "write failure", writer: func() controlledStreamWriter { return newFailingWriteStreamWriter() }},
		{name: "flush failure", writer: func() controlledStreamWriter { return newFailingFlushStreamWriter() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			broker := newHTTPTestBroker(t, 1)
			subscriber := &recordingEventSubscriber{broker: broker, subscriptions: make(chan *event.Subscription, 1)}
			writer := test.writer()
			done := make(chan struct{})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
			go func() {
				newEventStreamHandler(subscriber, time.Hour).ServeHTTP(writer, request)
				close(done)
			}()
			waitHTTPTestSignal(t, writer.initialFlush())
			subscription := waitHTTPTestValue(t, subscriber.subscriptions)

			if err := broker.Publish(event.IncidentOpened{ServiceID: "github", StartedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}

			waitHTTPTestSignal(t, done)
			assertEventChannelClosed(t, subscription.Events())
		})
	}
}

func TestMarshalEventFrameRejectsPointerEvents(t *testing.T) {
	if _, err := marshalEventFrame(&event.StatusChanged{}); err == nil {
		t.Fatal("marshalEventFrame(pointer) error = nil, want error")
	}
}

func TestMarshalEventFrameEscapesPayloadNewlines(t *testing.T) {
	frame, err := marshalEventFrame(event.StatusChanged{
		ServiceID:     "git\nhub",
		State:         status.StatusUp,
		LastCheckedAt: time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(frame), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[1], "data: ") || lines[2] != "" || lines[3] != "" {
		t.Fatalf("frame lines = %#v, want one event line, one data line, and blank terminator", lines)
	}
	if !strings.Contains(lines[1], `"serviceId":"git\nhub"`) {
		t.Fatalf("data line = %q, want escaped newline", lines[1])
	}
}

func TestEventStreamRejectsPostWithoutSubscribing(t *testing.T) {
	calls := 0
	subscriber := eventSubscriberFunc(func() (*event.Subscription, error) {
		calls++
		return nil, event.ErrClosed
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events", nil)
	response := httptest.NewRecorder()

	NewRouter(&fakeCurrentStatusReader{}, &fakeIncidentsReader{}, subscriber).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if calls != 0 {
		t.Fatalf("Subscribe calls = %d, want 0", calls)
	}
}

type unavailableEventSubscriber struct{}

func (unavailableEventSubscriber) Subscribe() (*event.Subscription, error) {
	return nil, event.ErrClosed
}

type eventSubscriberFunc func() (*event.Subscription, error)

func (f eventSubscriberFunc) Subscribe() (*event.Subscription, error) {
	return f()
}

type recordingEventSubscriber struct {
	broker        *event.Broker
	subscriptions chan *event.Subscription
}

func (s *recordingEventSubscriber) Subscribe() (*event.Subscription, error) {
	subscription, err := s.broker.Subscribe()
	if err == nil {
		s.subscriptions <- subscription
	}
	return subscription, err
}

type nonFlushingResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newNonFlushingResponseWriter() *nonFlushingResponseWriter {
	return &nonFlushingResponseWriter{header: make(http.Header)}
}

func (w *nonFlushingResponseWriter) Header() http.Header {
	return w.header
}

func (w *nonFlushingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *nonFlushingResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

type blockingStreamWriter struct {
	header         http.Header
	initialFlushed chan struct{}
	writeStarted   chan struct{}
	releaseWrite   chan struct{}
	flushOnce      sync.Once
	writeOnce      sync.Once
	releaseOnce    sync.Once
}

func newBlockingStreamWriter() *blockingStreamWriter {
	return &blockingStreamWriter{
		header:         make(http.Header),
		initialFlushed: make(chan struct{}),
		writeStarted:   make(chan struct{}),
		releaseWrite:   make(chan struct{}),
	}
}

func (w *blockingStreamWriter) Header() http.Header {
	return w.header
}

func (*blockingStreamWriter) WriteHeader(int) {}

func (w *blockingStreamWriter) Write(body []byte) (int, error) {
	w.writeOnce.Do(func() { close(w.writeStarted) })
	<-w.releaseWrite
	return len(body), nil
}

func (w *blockingStreamWriter) Flush() {
	w.flushOnce.Do(func() { close(w.initialFlushed) })
}

func (w *blockingStreamWriter) release() {
	w.releaseOnce.Do(func() { close(w.releaseWrite) })
}

type controlledStreamWriter interface {
	http.ResponseWriter
	http.Flusher
	initialFlush() <-chan struct{}
}

type failingWriteStreamWriter struct {
	header         http.Header
	initialFlushed chan struct{}
	flushOnce      sync.Once
}

func newFailingWriteStreamWriter() *failingWriteStreamWriter {
	return &failingWriteStreamWriter{header: make(http.Header), initialFlushed: make(chan struct{})}
}

func (w *failingWriteStreamWriter) Header() http.Header {
	return w.header
}

func (*failingWriteStreamWriter) WriteHeader(int) {}

func (*failingWriteStreamWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (w *failingWriteStreamWriter) Flush() {
	w.flushOnce.Do(func() { close(w.initialFlushed) })
}

func (w *failingWriteStreamWriter) initialFlush() <-chan struct{} {
	return w.initialFlushed
}

type failingFlushStreamWriter struct {
	header         http.Header
	initialFlushed chan struct{}
	flushCalls     int
}

func newFailingFlushStreamWriter() *failingFlushStreamWriter {
	return &failingFlushStreamWriter{header: make(http.Header), initialFlushed: make(chan struct{})}
}

func (w *failingFlushStreamWriter) Header() http.Header {
	return w.header
}

func (*failingFlushStreamWriter) WriteHeader(int) {}

func (*failingFlushStreamWriter) Write(body []byte) (int, error) {
	return len(body), nil
}

func (*failingFlushStreamWriter) Flush() {}

func (w *failingFlushStreamWriter) FlushError() error {
	w.flushCalls++
	if w.flushCalls == 1 {
		close(w.initialFlushed)
		return nil
	}
	return errors.New("flush failed")
}

func (w *failingFlushStreamWriter) initialFlush() <-chan struct{} {
	return w.initialFlushed
}

func newHTTPTestBroker(t *testing.T, capacity int) *event.Broker {
	t.Helper()
	broker, err := event.NewBroker(capacity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	return broker
}

func openEventStream(t *testing.T, handler http.Handler) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), eventHTTPTestTimeout)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = response.Body.Close()
	})
	return response, bufio.NewReader(response.Body), cancel
}

func readSSERecord(t *testing.T, reader *bufio.Reader) []string {
	t.Helper()
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE record: %v", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			return lines
		}
		lines = append(lines, line)
	}
}

func waitHTTPTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(eventHTTPTestTimeout):
		t.Fatal("timed out waiting for signal")
	}
}

func waitHTTPTestValue[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(eventHTTPTestTimeout):
		t.Fatal("timed out waiting for value")
		var zero T
		return zero
	}
}

func assertEventChannelClosed(t *testing.T, events <-chan event.Event) {
	t.Helper()
	select {
	case value, ok := <-events:
		if ok {
			t.Fatalf("received event %#v, want closed channel", value)
		}
	case <-time.After(eventHTTPTestTimeout):
		t.Fatal("timed out waiting for event channel closure")
	}
}
