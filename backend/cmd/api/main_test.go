package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/app"
	"github.com/jowongx8/backend/internal/event"
	"github.com/jowongx8/backend/internal/httpapi"
	"github.com/jowongx8/backend/internal/status"
	"github.com/jowongx8/backend/internal/storage/sqlite"
)

const supervisorTestTimeout = 5 * time.Second

func TestRunFailsBeforeStartingServicesWhenDatabaseInitializationFails(t *testing.T) {
	t.Chdir(t.TempDir())
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_PATH", filepath.Join(parent, "pulsegrid.db"))

	err := run(context.Background(), testLogger())
	if err == nil || !strings.Contains(err.Error(), "database initialization failed") {
		t.Fatalf("run() error = %v, want database startup error", err)
	}
}

func TestRunFailsBeforeStartingServicesWhenIncidentRestorationFails(t *testing.T) {
	t.Chdir(t.TempDir())
	databasePath := filepath.Join(t.TempDir(), "pulsegrid.db")
	db, err := sqlite.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO incidents (service_id, started_at_ms, resolved_at_ms)
		VALUES (?, ?, NULL)
	`, "github", "not-a-timestamp"); err != nil {
		t.Fatalf("insert malformed incident: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	t.Setenv("DATABASE_PATH", databasePath)
	t.Setenv("PORT", strconv.Itoa(occupied.Addr().(*net.TCPAddr).Port))

	err = run(context.Background(), testLogger())
	if err == nil || !strings.Contains(err.Error(), "restore open incidents") || !strings.Contains(err.Error(), "scan open incident") {
		t.Fatalf("run() error = %v, want incident restoration failure", err)
	}
}

func TestRunApplicationStopsBothSubsystemsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitoringStarted := make(chan struct{})
	monitoringStopped := make(chan struct{})
	httpShutdown := make(chan struct{})
	server := testServer()
	server.RegisterOnShutdown(func() { close(httpShutdown) })
	done := make(chan error, 1)
	go func() {
		done <- runApplication(ctx, server, func(ctx context.Context) error {
			close(monitoringStarted)
			<-ctx.Done()
			close(monitoringStopped)
			return nil
		}, testLogger())
	}()

	waitForSignal(t, monitoringStarted)
	cancel()
	if err := waitForError(t, done); err != nil {
		t.Fatalf("runApplication() after cancellation = %v, want nil", err)
	}
	waitForSignal(t, monitoringStopped)
	waitForSignal(t, httpShutdown)
}

func TestRunApplicationCancelsMonitoringOnHTTPFailure(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	monitoringStopped := make(chan struct{})
	server := &http.Server{Addr: occupied.Addr().String(), Handler: httpapi.NewRouter(emptyCurrentStatusReader{}, emptyIncidentsReader{}, unavailableEventSubscriber{})}
	done := make(chan error, 1)
	go func() {
		done <- runApplication(context.Background(), server, func(ctx context.Context) error {
			<-ctx.Done()
			close(monitoringStopped)
			return nil
		}, testLogger())
	}()

	if err := waitForError(t, done); err == nil || !strings.Contains(err.Error(), "HTTP server") {
		t.Fatalf("runApplication() error = %v, want HTTP listen failure", err)
	}
	waitForSignal(t, monitoringStopped)
}

func TestRunApplicationStopsHTTPOnMonitoringFailure(t *testing.T) {
	wantErr := fmt.Errorf("publish status changed: %w", event.ErrClosed)
	httpShutdown := make(chan struct{})
	server := testServer()
	server.RegisterOnShutdown(func() { close(httpShutdown) })

	done := make(chan error, 1)
	go func() {
		done <- runApplication(context.Background(), server, func(context.Context) error {
			return wantErr
		}, testLogger())
	}()

	if err := waitForError(t, done); !errors.Is(err, wantErr) || !errors.Is(err, event.ErrClosed) {
		t.Fatalf("runApplication() error = %v, want publication failure", err)
	}
	waitForSignal(t, httpShutdown)
}

func TestRunApplicationRejectsUnexpectedMonitoringExit(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- runApplication(context.Background(), testServer(), func(context.Context) error {
			return nil
		}, testLogger())
	}()
	if err := waitForError(t, done); err == nil || !strings.Contains(err.Error(), "monitoring runtime stopped unexpectedly") {
		t.Fatalf("runApplication() error = %v, want unexpected monitoring exit", err)
	}
}

func TestRunApplicationCancelsSSEBeforeCompositionClosesBroker(t *testing.T) {
	broker, err := event.NewBroker(event.DefaultSubscriberBufferSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	rootCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	monitoringContext := make(chan context.Context, 1)
	publishResult := make(chan error, 1)
	server := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: httpapi.NewRouter(emptyCurrentStatusReader{}, emptyIncidentsReader{}, broker),
	}
	applicationDone := make(chan error, 1)
	go func() {
		applicationDone <- runApplication(rootCtx, server, func(ctx context.Context) error {
			monitoringContext <- ctx
			<-ctx.Done()
			publishResult <- broker.Publish(event.StatusChanged{
				ServiceID:     "github",
				State:         status.StatusUp,
				LastCheckedAt: time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC),
			})
			return nil
		}, testLogger())
	}()

	appCtx := waitForContext(t, monitoringContext)
	if server.BaseContext == nil {
		t.Fatal("server BaseContext was not configured")
	}
	requestCtx := server.BaseContext(nil)
	if requestCtx != appCtx {
		t.Fatal("request base context does not match monitoring application context")
	}

	response := newApplicationStreamWriter()
	handlerDone := make(chan struct{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(requestCtx)
	go func() {
		server.Handler.ServeHTTP(response, request)
		close(handlerDone)
	}()
	waitForSignal(t, response.initialFlushed)

	cancel()
	waitForPromptSignal(t, handlerDone)
	if err := waitForError(t, applicationDone); err != nil {
		t.Fatalf("runApplication() after cancellation = %v, want nil", err)
	}
	if err := waitForPublishResult(t, publishResult); err != nil {
		t.Fatalf("publication after cancellation = %v, want nil", err)
	}

	subscription, err := broker.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe() after runApplication = %v, want broker open", err)
	}
	subscription.Close()

	broker.Close()
	if err := broker.Publish(event.IncidentOpened{ServiceID: "github", StartedAt: time.Now()}); !errors.Is(err, event.ErrClosed) {
		t.Fatalf("Publish() after composition close = %v, want ErrClosed", err)
	}
}

func testServer() *http.Server {
	return &http.Server{Addr: "127.0.0.1:0", Handler: httpapi.NewRouter(emptyCurrentStatusReader{}, emptyIncidentsReader{}, unavailableEventSubscriber{})}
}

type unavailableEventSubscriber struct{}

func (unavailableEventSubscriber) Subscribe() (*event.Subscription, error) {
	return nil, event.ErrClosed
}

type applicationStreamWriter struct {
	header         http.Header
	initialFlushed chan struct{}
	flushOnce      sync.Once
}

func newApplicationStreamWriter() *applicationStreamWriter {
	return &applicationStreamWriter{
		header:         make(http.Header),
		initialFlushed: make(chan struct{}),
	}
}

func (w *applicationStreamWriter) Header() http.Header {
	return w.header
}

func (*applicationStreamWriter) WriteHeader(int) {}

func (*applicationStreamWriter) Write(body []byte) (int, error) {
	return len(body), nil
}

func (w *applicationStreamWriter) Flush() {
	w.flushOnce.Do(func() { close(w.initialFlushed) })
}

type emptyCurrentStatusReader struct{}

func (emptyCurrentStatusReader) Execute() []app.ServiceStatusView {
	return []app.ServiceStatusView{}
}

type emptyIncidentsReader struct{}

func (emptyIncidentsReader) Execute(context.Context) (app.IncidentsView, error) {
	return app.IncidentsView{Active: []app.IncidentView{}, Resolved: []app.IncidentView{}}, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(supervisorTestTimeout):
		t.Fatal("timed out waiting for signal")
	}
}

func waitForPromptSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt shutdown")
	}
}

func waitForContext(t *testing.T, contexts <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-contexts:
		return ctx
	case <-time.After(supervisorTestTimeout):
		t.Fatal("timed out waiting for application context")
		return nil
	}
}

func waitForPublishResult(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(supervisorTestTimeout):
		t.Fatal("timed out waiting for publication result")
		return nil
	}
}

func waitForError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(supervisorTestTimeout):
		t.Fatal("timed out waiting for application completion")
		return nil
	}
}
