package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestOpenCreatesDatabaseAndAppliesMigrationsOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "pulse?grid#1.db")

	for attempt := 1; attempt <= 2; attempt++ {
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("Open() attempt %d: %v", attempt, err)
		}

		if _, err := os.Stat(path); err != nil {
			t.Errorf("database file after attempt %d: %v", attempt, err)
		}
		var applied int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM goose_db_version WHERE version_id = 1 AND is_applied = 1").Scan(&applied); err != nil {
			t.Errorf("query migration version after attempt %d: %v", attempt, err)
		} else if applied != 1 {
			t.Errorf("applied version-1 records after attempt %d = %d, want 1", attempt, applied)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM goose_db_version WHERE version_id = 2 AND is_applied = 1").Scan(&applied); err != nil {
			t.Errorf("query migration version 2 after attempt %d: %v", attempt, err)
		} else if applied != 1 {
			t.Errorf("applied version-2 records after attempt %d = %d, want 1", attempt, applied)
		}
		var latest int
		if err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&latest); err != nil {
			t.Errorf("query latest migration version after attempt %d: %v", attempt, err)
		} else if latest != 2 {
			t.Errorf("latest migration version after attempt %d = %d, want 2", attempt, latest)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close() attempt %d: %v", attempt, err)
		}
	}
}

func TestOpenConfiguresEveryPooledConnection(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "pulsegrid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got != maxConnections {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, maxConnections)
	}

	// Holding each sql.Conn forces the pool to open four distinct physical connections.
	conns := make([]*sql.Conn, 0, maxConnections)
	defer func() {
		for _, conn := range conns {
			if conn == nil {
				continue
			}
			if err := conn.Close(); err != nil {
				t.Errorf("close connection: %v", err)
			}
		}
	}()
	for range maxConnections {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire connection: %v", err)
		}
		conns = append(conns, conn)
	}

	if got := db.Stats().OpenConnections; got != maxConnections {
		t.Fatalf("OpenConnections = %d, want %d", got, maxConnections)
	}
	for i, conn := range conns {
		var mode string
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || !strings.EqualFold(mode, "wal") {
			t.Errorf("connection %d journal_mode = %q, err = %v; want wal", i, mode, err)
		}
		for _, tc := range []struct {
			pragma string
			want   int
		}{
			{"synchronous", 1},
			{"foreign_keys", 1},
			{"busy_timeout", 5000},
		} {
			var got int
			if err := conn.QueryRowContext(ctx, "PRAGMA "+tc.pragma).Scan(&got); err != nil || got != tc.want {
				t.Errorf("connection %d %s = %d, err = %v; want %d", i, tc.pragma, got, err, tc.want)
			}
		}
	}

	for i, conn := range conns {
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		conns[i] = nil
	}
	if got := db.Stats().Idle; got != maxConnections {
		t.Errorf("Idle connections = %d, want %d", got, maxConnections)
	}
}

func TestOpenRejectsUnusablePath(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(context.Background(), filepath.Join(parent, "pulsegrid.db")); err == nil || db != nil {
		t.Fatalf("Open() = (%v, %v), want directory error and nil database", db, err)
	}
}

func TestOpenRejectsInvalidSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pulsegrid.db")
	if err := os.WriteFile(path, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(context.Background(), path); err == nil || db != nil {
		t.Fatalf("Open() = (%v, %v), want SQLite setup error and nil database", db, err)
	}
}

func TestOpenFailsOnMigrationErrorWithoutRecordingVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pulsegrid.db")
	badMigrations := fstest.MapFS{
		"00001_bad.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT value FROM missing_table;\n\n-- +goose Down\nSELECT 1;\n")},
	}
	if db, err := open(ctx, path, badMigrations); err == nil || db != nil || !strings.Contains(err.Error(), "run SQLite migrations") {
		t.Fatalf("open() = (%v, %v), want migration error and nil database", db, err)
	}

	db, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var applied int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM goose_db_version WHERE version_id = 1 AND is_applied = 1").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatalf("applied version-1 records = %d, want 0", applied)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ready, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() after failed migration: %v", err)
	}
	defer ready.Close()
}
