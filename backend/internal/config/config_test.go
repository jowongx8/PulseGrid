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
	writeEnvFile(t, "PORT=3000\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Port != 3000 {
		t.Fatalf("Port = %d, want 3000", cfg.Port)
	}
}

func TestLoadProcessEnvTakesPrecedenceOverEnvFile(t *testing.T) {
	withCleanConfigEnv(t)
	writeEnvFile(t, "PORT=8080\n")
	t.Setenv("PORT", "9000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Port != 9000 {
		t.Fatalf("Port = %d, want 9000", cfg.Port)
	}
}

func withCleanConfigEnv(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())
	unsetEnv(t, "PORT")
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
