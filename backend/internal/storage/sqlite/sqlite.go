package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const maxConnections = 4

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Open returns a database only after its SQLite settings and migrations are ready.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("load embedded migrations: %w", err)
	}
	return open(ctx, path, migrations)
}

func open(ctx context.Context, path string, migrations fs.FS) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("database path must not be empty")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, fmt.Errorf("prepare database directory: %w", err)
	}

	db, err := sql.Open("sqlite", databaseURI(absPath))
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections)

	if err := initialize(ctx, db, migrations); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close SQLite database after initialization failure: %w", closeErr))
		}
		return nil, err
	}
	return db, nil
}

func databaseURI(path string) string {
	uriPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{}
	// These driver options are applied whenever database/sql opens a physical connection.
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "on")
	query.Set("_synchronous", "normal")
	uri.RawQuery = query.Encode()
	return uri.String()
}

func initialize(ctx context.Context, db *sql.DB, migrations fs.FS) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("validate SQLite connection: %w", err)
	}

	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return fmt.Errorf("configure SQLite WAL mode: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("configure SQLite WAL mode: got %q", journalMode)
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("prepare SQLite migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run SQLite migrations: %w", err)
	}
	return nil
}
