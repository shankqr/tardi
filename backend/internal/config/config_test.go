package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Clear env vars that would override defaults
	for _, key := range []string{"PORT", "DATABASE_URL", "ALLOWED_ORIGINS", "ENVIRONMENT", "LOG_LEVEL", "API_URL", "OPENCLAW_IMAGE_TAG"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("Port: got %q, want %q", cfg.Port, "8080")
	}
	if cfg.DatabaseURL != "postgres://tardi:tardi@localhost:5432/tardi?sslmode=disable" {
		t.Errorf("DatabaseURL: got %q, want default localhost URL", cfg.DatabaseURL)
	}
	if cfg.Environment != "dev" {
		t.Errorf("Environment: got %q, want %q", cfg.Environment, "dev")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.APIURL != "http://localhost:8080" {
		t.Errorf("APIURL: got %q, want %q", cfg.APIURL, "http://localhost:8080")
	}
	if cfg.OpenClawImageTag != "latest" {
		t.Errorf("OpenClawImageTag: got %q, want %q", cfg.OpenClawImageTag, "latest")
	}
}

func TestLoadPortEnvVar(t *testing.T) {
	t.Setenv("PORT", "3000")

	cfg := Load()

	if cfg.Port != "3000" {
		t.Errorf("Port: got %q, want %q", cfg.Port, "3000")
	}
}

func TestLoadAllowedOriginsSplit(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:5173,https://app.tardi.ai,https://dev.tardi.ai")

	cfg := Load()

	if len(cfg.AllowedOrigins) != 3 {
		t.Fatalf("AllowedOrigins: got %d items, want 3", len(cfg.AllowedOrigins))
	}
	expected := []string{"http://localhost:5173", "https://app.tardi.ai", "https://dev.tardi.ai"}
	for i, want := range expected {
		if cfg.AllowedOrigins[i] != want {
			t.Errorf("AllowedOrigins[%d]: got %q, want %q", i, cfg.AllowedOrigins[i], want)
		}
	}
}

func TestIsDevTrue(t *testing.T) {
	cfg := &Config{Environment: "dev"}
	if !cfg.IsDev() {
		t.Error("IsDev() should return true for 'dev'")
	}
}

func TestIsDevFalse(t *testing.T) {
	cfg := &Config{Environment: "prod"}
	if cfg.IsDev() {
		t.Error("IsDev() should return false for 'prod'")
	}
}

func TestSecretOrEmptyPlaceholder(t *testing.T) {
	t.Setenv("HETZNER_API_TOKEN", "PLACEHOLDER")

	cfg := Load()

	if cfg.HetznerAPIToken != "" {
		t.Errorf("HetznerAPIToken: got %q, want empty string for PLACEHOLDER", cfg.HetznerAPIToken)
	}
}

func TestMockInitDelayParsed(t *testing.T) {
	t.Setenv("MOCK_INIT_DELAY", "5s")

	cfg := Load()

	if cfg.MockInitDelay != 5*time.Second {
		t.Errorf("MockInitDelay: got %v, want 5s", cfg.MockInitDelay)
	}
}

func TestMockInitDelayDefault(t *testing.T) {
	// Ensure env var is not set
	t.Setenv("MOCK_INIT_DELAY", "")
	os.Unsetenv("MOCK_INIT_DELAY")

	cfg := Load()

	if cfg.MockInitDelay != 12*time.Second {
		t.Errorf("MockInitDelay default: got %v, want 12s", cfg.MockInitDelay)
	}
}

func TestEnvironmentEnvVar(t *testing.T) {
	t.Setenv("ENVIRONMENT", "prod")

	cfg := Load()

	if cfg.Environment != "prod" {
		t.Errorf("Environment: got %q, want %q", cfg.Environment, "prod")
	}
}
