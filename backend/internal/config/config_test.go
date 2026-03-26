package config

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

// setEnv sets an env var for the duration of the test and restores it on cleanup.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	os.Setenv(key, value)
	t.Cleanup(func() {
		if existed {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	})
}

// unsetEnv ensures an env var is unset for the duration of the test.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if existed {
			os.Setenv(key, old)
		}
	})
}

func TestLoadDefaults(t *testing.T) {
	// Unset all env vars that Load() reads so we get pure defaults.
	envVars := []string{
		"PORT", "DATABASE_URL", "ALLOWED_ORIGINS", "ENVIRONMENT",
		"LOG_LEVEL", "API_URL", "OPENCLAW_IMAGE_TAG",
		"FIREBASE_PROJECT_ID", "STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET",
		"HETZNER_API_TOKEN", "CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ZONE_ID",
		"CLOUDFLARE_BASE_DOMAIN", "ADMIN_API_TOKEN",
		"GOOGLE_OAUTH_CLIENT_ID", "GOOGLE_OAUTH_CLIENT_SECRET",
		"TOKEN_ENCRYPTION_KEY", "BACKEND_EGRESS_CIDRS", "SSH_PRIVATE_KEY",
		"MOCK_INIT_DELAY", "MOCK_HEARTBEAT_DELAY", "MOCK_STOP_DELAY",
		"MOCK_START_DELAY", "MOCK_RESTART_DELAY",
	}
	for _, key := range envVars {
		unsetEnv(t, key)
	}

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.DatabaseURL != "postgres://tardi:tardi@localhost:5432/tardi?sslmode=disable" {
		t.Errorf("DatabaseURL = %q, want default", cfg.DatabaseURL)
	}
	if cfg.Environment != "dev" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "dev")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.OpenClawImageTag != "latest" {
		t.Errorf("OpenClawImageTag = %q, want %q", cfg.OpenClawImageTag, "latest")
	}
	if cfg.APIURL != "http://localhost:8080" {
		t.Errorf("APIURL = %q, want %q", cfg.APIURL, "http://localhost:8080")
	}
}

func TestLoadWithEnvOverrides(t *testing.T) {
	setEnv(t, "PORT", "9090")
	setEnv(t, "ENVIRONMENT", "prod")
	setEnv(t, "LOG_LEVEL", "debug")
	setEnv(t, "OPENCLAW_IMAGE_TAG", "v1.2.3")
	setEnv(t, "API_URL", "https://api.tardi.ai")
	// Unset SSH key to avoid parse errors
	unsetEnv(t, "SSH_PRIVATE_KEY")

	cfg := Load()

	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
	if cfg.Environment != "prod" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "prod")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.OpenClawImageTag != "v1.2.3" {
		t.Errorf("OpenClawImageTag = %q, want %q", cfg.OpenClawImageTag, "v1.2.3")
	}
	if cfg.APIURL != "https://api.tardi.ai" {
		t.Errorf("APIURL = %q, want %q", cfg.APIURL, "https://api.tardi.ai")
	}
}

func TestAllowedOriginsCommaSplitting(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "single origin",
			value: "https://tardi.ai",
			want:  []string{"https://tardi.ai"},
		},
		{
			name:  "multiple origins",
			value: "https://tardi.ai,https://dev.tardi.ai,http://localhost:5173",
			want:  []string{"https://tardi.ai", "https://dev.tardi.ai", "http://localhost:5173"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, "ALLOWED_ORIGINS", tt.value)
			unsetEnv(t, "SSH_PRIVATE_KEY")
			cfg := Load()

			if len(cfg.AllowedOrigins) != len(tt.want) {
				t.Fatalf("AllowedOrigins len = %d, want %d", len(cfg.AllowedOrigins), len(tt.want))
			}
			for i, got := range cfg.AllowedOrigins {
				if got != tt.want[i] {
					t.Errorf("AllowedOrigins[%d] = %q, want %q", i, got, tt.want[i])
				}
			}
		})
	}
}

func TestAllowedOriginsDefault(t *testing.T) {
	unsetEnv(t, "ALLOWED_ORIGINS")
	unsetEnv(t, "SSH_PRIVATE_KEY")
	cfg := Load()

	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "http://localhost:5173" {
		t.Errorf("AllowedOrigins default = %v, want [http://localhost:5173]", cfg.AllowedOrigins)
	}
}

func TestSecretOrEmpty(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"placeholder treated as empty", "PLACEHOLDER", ""},
		{"real value preserved", "sk_live_abc123", "sk_live_abc123"},
		{"empty string stays empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_SECRET_OR_EMPTY"
			if tt.value != "" {
				setEnv(t, key, tt.value)
			} else {
				unsetEnv(t, key)
			}
			got := secretOrEmpty(key)
			if got != tt.want {
				t.Errorf("secretOrEmpty() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsDev(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"dev environment", "dev", true},
		{"prod environment", "prod", false},
		{"staging environment", "staging", false},
		{"empty defaults to dev", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Environment: tt.env}
			// For the "empty defaults to dev" case, test via Load()
			if tt.env == "" {
				unsetEnv(t, "ENVIRONMENT")
				unsetEnv(t, "SSH_PRIVATE_KEY")
				cfg = Load()
			}
			if got := cfg.IsDev(); got != tt.want {
				t.Errorf("IsDev() = %v, want %v (env=%q)", got, tt.want, tt.env)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		fallback time.Duration
		want     time.Duration
	}{
		{"valid duration", "5s", 10 * time.Second, 5 * time.Second},
		{"valid ms duration", "500ms", 10 * time.Second, 500 * time.Millisecond},
		{"valid minute duration", "2m", 10 * time.Second, 2 * time.Minute},
		{"invalid falls back", "notaduration", 10 * time.Second, 10 * time.Second},
		{"empty falls back", "", 12 * time.Second, 12 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_PARSE_DURATION"
			if tt.envValue != "" {
				setEnv(t, key, tt.envValue)
			} else {
				unsetEnv(t, key)
			}
			got := parseDuration(key, tt.fallback)
			if got != tt.want {
				t.Errorf("parseDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadMockDelaysFromEnv(t *testing.T) {
	setEnv(t, "MOCK_INIT_DELAY", "1s")
	setEnv(t, "MOCK_HEARTBEAT_DELAY", "2s")
	setEnv(t, "MOCK_STOP_DELAY", "500ms")
	setEnv(t, "MOCK_START_DELAY", "750ms")
	setEnv(t, "MOCK_RESTART_DELAY", "3s")
	unsetEnv(t, "SSH_PRIVATE_KEY")

	cfg := Load()

	if cfg.MockInitDelay != 1*time.Second {
		t.Errorf("MockInitDelay = %v, want 1s", cfg.MockInitDelay)
	}
	if cfg.MockHeartbeatDelay != 2*time.Second {
		t.Errorf("MockHeartbeatDelay = %v, want 2s", cfg.MockHeartbeatDelay)
	}
	if cfg.MockStopDelay != 500*time.Millisecond {
		t.Errorf("MockStopDelay = %v, want 500ms", cfg.MockStopDelay)
	}
	if cfg.MockStartDelay != 750*time.Millisecond {
		t.Errorf("MockStartDelay = %v, want 750ms", cfg.MockStartDelay)
	}
	if cfg.MockRestartDelay != 3*time.Second {
		t.Errorf("MockRestartDelay = %v, want 3s", cfg.MockRestartDelay)
	}
}

func TestLoadSSHPrivateKey(t *testing.T) {
	t.Run("no key set", func(t *testing.T) {
		unsetEnv(t, "SSH_PRIVATE_KEY")
		got := loadSSHPrivateKey()
		if got != nil {
			t.Errorf("loadSSHPrivateKey() = %v, want nil", got)
		}
	})

	t.Run("placeholder treated as empty", func(t *testing.T) {
		setEnv(t, "SSH_PRIVATE_KEY", "PLACEHOLDER")
		got := loadSSHPrivateKey()
		if got != nil {
			t.Errorf("loadSSHPrivateKey() = %v, want nil", got)
		}
	})

	t.Run("valid base64 decoded", func(t *testing.T) {
		pemData := "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----\n"
		encoded := base64.StdEncoding.EncodeToString([]byte(pemData))
		setEnv(t, "SSH_PRIVATE_KEY", encoded)
		got := loadSSHPrivateKey()
		if string(got) != pemData {
			t.Errorf("loadSSHPrivateKey() = %q, want %q", got, pemData)
		}
	})

	t.Run("invalid base64 returns nil", func(t *testing.T) {
		setEnv(t, "SSH_PRIVATE_KEY", "not-valid-base64!!!")
		got := loadSSHPrivateKey()
		if got != nil {
			t.Errorf("loadSSHPrivateKey() with invalid base64 = %v, want nil", got)
		}
	})
}

func TestEnvOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		set      bool
		fallback string
		want     string
	}{
		{"env set", "custom", true, "default", "custom"},
		{"env not set", "", false, "default", "default"},
		{"env set to empty", "", true, "default", "default"}, // empty treated as unset
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_ENV_OR_DEFAULT"
			if tt.set {
				setEnv(t, key, tt.envValue)
			} else {
				unsetEnv(t, key)
			}
			got := envOrDefault(key, tt.fallback)
			if got != tt.want {
				t.Errorf("envOrDefault() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSecretOrEmptyWithHetznerToken(t *testing.T) {
	// Integration-style: verify PLACEHOLDER Hetzner token results in empty config
	setEnv(t, "HETZNER_API_TOKEN", "PLACEHOLDER")
	unsetEnv(t, "SSH_PRIVATE_KEY")
	cfg := Load()

	if cfg.HetznerAPIToken != "" {
		t.Errorf("HetznerAPIToken with PLACEHOLDER = %q, want empty", cfg.HetznerAPIToken)
	}
}

func TestSecretOrEmptyWithRealToken(t *testing.T) {
	setEnv(t, "HETZNER_API_TOKEN", "real-token-value")
	unsetEnv(t, "SSH_PRIVATE_KEY")
	cfg := Load()

	if cfg.HetznerAPIToken != "real-token-value" {
		t.Errorf("HetznerAPIToken = %q, want %q", cfg.HetznerAPIToken, "real-token-value")
	}
}

func TestBackendEgressCIDRs(t *testing.T) {
	setEnv(t, "BACKEND_EGRESS_CIDRS", "1.2.3.4/32,5.6.7.8/32")
	unsetEnv(t, "SSH_PRIVATE_KEY")
	cfg := Load()

	if !strings.Contains(cfg.BackendEgressCIDRs, "1.2.3.4/32") {
		t.Errorf("BackendEgressCIDRs = %q, want to contain CIDRs", cfg.BackendEgressCIDRs)
	}
}
