package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/httpapi"
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
	server := &http.Server{Addr: occupied.Addr().String(), Handler: httpapi.NewRouter()}
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
	wantErr := errors.New("monitoring failed")
	httpShutdown := make(chan struct{})
	server := testServer()
	server.RegisterOnShutdown(func() { close(httpShutdown) })

	done := make(chan error, 1)
	go func() {
		done <- runApplication(context.Background(), server, func(context.Context) error {
			return wantErr
		}, testLogger())
	}()

	if err := waitForError(t, done); !errors.Is(err, wantErr) {
		t.Fatalf("runApplication() error = %v, want monitoring failure", err)
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

func testServer() *http.Server {
	return &http.Server{Addr: "127.0.0.1:0", Handler: httpapi.NewRouter()}
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
