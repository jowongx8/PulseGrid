package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/httpapi"
)

const supervisorTestTimeout = 5 * time.Second

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
