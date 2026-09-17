package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jowongx8/backend/internal/app"
	"github.com/jowongx8/backend/internal/config"
	"github.com/jowongx8/backend/internal/httpapi"
	"github.com/jowongx8/backend/internal/monitoring"
	"github.com/jowongx8/backend/internal/service"
	"github.com/jowongx8/backend/internal/storage/sqlite"
)

const (
	readHeaderTimeout    = 5 * time.Second
	shutdownTimeout      = 5 * time.Second
	monitorInterval      = 15 * time.Second
	monitorWorkers       = 4
	monitorQueueCapacity = 4
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) (retErr error) {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration failed: %w", err)
	}
	db, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close SQLite database: %w", err))
		}
	}()

	services := service.Catalogue()
	checker := monitoring.NewHTTPChecker()
	writer := sqlite.NewCheckResultStore(db)
	runtime, err := app.NewMonitoringRuntime(services, checker, writer, monitorInterval, monitorWorkers, monitorQueueCapacity)
	if err != nil {
		return fmt.Errorf("create monitoring runtime: %w", err)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpapi.NewRouter(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	return runApplication(ctx, server, runtime.Run, logger)
}

func runApplication(
	ctx context.Context,
	server *http.Server,
	runMonitoring func(context.Context) error,
	logger *slog.Logger,
) error {
	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var shutdownStarted atomic.Bool
	serverDone := make(chan error, 1)
	monitoringDone := make(chan error, 1)

	logger.Info("starting server", "addr", server.Addr)
	go func() {
		err := server.ListenAndServe()
		switch {
		case errors.Is(err, http.ErrServerClosed) && shutdownStarted.Load():
			err = nil
		case errors.Is(err, http.ErrServerClosed):
			err = errors.New("HTTP server stopped before shutdown")
		case err == nil:
			err = errors.New("HTTP server stopped unexpectedly")
		default:
			err = fmt.Errorf("HTTP server: %w", err)
		}
		serverDone <- err
	}()

	logger.Info("starting monitoring runtime")
	go func() {
		err := runMonitoring(appCtx)
		if err != nil {
			err = fmt.Errorf("monitoring runtime: %w", err)
		} else if appCtx.Err() == nil {
			err = errors.New("monitoring runtime stopped unexpectedly")
		}
		monitoringDone <- err
	}()

	var firstErr error
	serverSeen, monitoringSeen := false, false
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverDone:
		serverSeen = true
		firstErr = err
	case err := <-monitoringDone:
		monitoringSeen = true
		firstErr = err
	}

	logger.Info("application shutdown starting")
	cancel()
	shutdownStarted.Store(true)
	shutdownCtx, stopShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	shutdownErr := server.Shutdown(shutdownCtx)
	stopShutdown()
	if shutdownErr != nil {
		shutdownErr = fmt.Errorf("HTTP server shutdown: %w", shutdownErr)
		if closeErr := server.Close(); closeErr != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("HTTP server close: %w", closeErr))
		}
	}

	if !serverSeen {
		firstErr = errors.Join(firstErr, <-serverDone)
	}
	if !monitoringSeen {
		firstErr = errors.Join(firstErr, <-monitoringDone)
	}
	firstErr = errors.Join(firstErr, shutdownErr)
	if firstErr != nil {
		return firstErr
	}
	logger.Info("application shutdown complete")
	return nil
}
