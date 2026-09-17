package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultPort(t *testing.T) {
	withCleanConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Fatalf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.DatabasePath != "./data/pulsegrid.db" {
		t.Fatalf("DatabasePath = %q, want default path", cfg.DatabasePath)
	}
}

func TestLoadConfiguredDatabasePath(t *testing.T) {
	withCleanConfigEnv(t)
	t.Setenv("DATABASE_PATH", "./custom/pulsegrid.db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.DatabasePath != "./custom/pulsegrid.db" {
		t.Fatalf("DatabasePath = %q, want configured path", cfg.DatabasePath)
	}
}

func TestLoadEmptyDatabasePath(t *testing.T) {
	withCleanConfigEnv(t)
	t.Setenv("DATABASE_PATH", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() returned nil error, want invalid DATABASE_PATH error")
	}
}

func TestLoadValidConfiguredPort(t *testing.T) {
	withCleanConfigEnv(t)
	t.Setenv("PORT", "9000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Port != 9000 {
		t.Fatalf("Port = %d, want 9000", cfg.Port)
	}
}

func TestLoadInvalidConfiguredPort(t *testing.T) {
	tests := []string{
		"abc",
		"-1",
		"0",
		"70000",
	}

	for _, port := range tests {
		t.Run(port, func(t *testing.T) {
			withCleanConfigEnv(t)
			t.Setenv("PORT", port)

			if _, err := Load(); err == nil {
				t.Fatal("Load() returned nil error, want invalid PORT error")
			}
		})
	}
}

func TestLoadEnvFile(t *testing.T) {
	withCleanConfigEnv(t)
	writeEnvFile(t, "PORT=3000\nDATABASE_PATH=./from-env-file.db\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Port != 3000 {
		t.Fatalf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.DatabasePath != "./from-env-file.db" {
		t.Fatalf("DatabasePath = %q, want .env value", cfg.DatabasePath)
	}
}

func TestLoadProcessEnvTakesPrecedenceOverEnvFile(t *testing.T) {
	withCleanConfigEnv(t)
	writeEnvFile(t, "PORT=8080\nDATABASE_PATH=./from-env-file.db\n")
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_PATH", "./from-process.db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Port != 9000 {
		t.Fatalf("Port = %d, want 9000", cfg.Port)
	}
	if cfg.DatabasePath != "./from-process.db" {
		t.Fatalf("DatabasePath = %q, want process value", cfg.DatabasePath)
	}
}

func withCleanConfigEnv(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())
	unsetEnv(t, "PORT")
	unsetEnv(t, "DATABASE_PATH")
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()

	value, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%q) returned error: %v", key, err)
	}

	t.Cleanup(func() {
		if ok {
			if err := os.Setenv(key, value); err != nil {
				t.Errorf("Setenv(%q) cleanup returned error: %v", key, err)
			}
			return
		}

		if err := os.Unsetenv(key); err != nil {
			t.Errorf("Unsetenv(%q) cleanup returned error: %v", key, err)
		}
	})
}

func writeEnvFile(t *testing.T, contents string) {
	t.Helper()

	path := filepath.Join(".", ".env")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}
}
