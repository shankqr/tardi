package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Port                string
	DatabaseURL         string
	AllowedOrigins      []string
	FirebaseProjectID   string
	StripeSecretKey     string
	StripeWebhookSecret string
	HetznerAPIToken     string
	Environment         string
	LogLevel            string
	APIURL              string

	// Mock provider delays (dev only)
	MockInitDelay      time.Duration
	MockHeartbeatDelay time.Duration
	MockStopDelay      time.Duration
	MockStartDelay     time.Duration
	MockRestartDelay   time.Duration
}

func Load() *Config {
	return &Config{
		Port:               envOrDefault("PORT", "8080"),
		DatabaseURL:        envOrDefault("DATABASE_URL", "postgres://tardi:tardi@localhost:5432/tardi?sslmode=disable"),
		AllowedOrigins:     strings.Split(envOrDefault("ALLOWED_ORIGINS", "http://localhost:5173"), ","),
		FirebaseProjectID:  os.Getenv("FIREBASE_PROJECT_ID"),
		StripeSecretKey:    os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		HetznerAPIToken:    secretOrEmpty("HETZNER_API_TOKEN"),
		Environment:        envOrDefault("ENVIRONMENT", "dev"),
		LogLevel:           envOrDefault("LOG_LEVEL", "info"),
		APIURL:             envOrDefault("API_URL", "http://localhost:8080"),

		MockInitDelay:      parseDuration("MOCK_INIT_DELAY", 12*time.Second),
		MockHeartbeatDelay: parseDuration("MOCK_HEARTBEAT_DELAY", 18*time.Second),
		MockStopDelay:      parseDuration("MOCK_STOP_DELAY", 3*time.Second),
		MockStartDelay:     parseDuration("MOCK_START_DELAY", 5*time.Second),
		MockRestartDelay:   parseDuration("MOCK_RESTART_DELAY", 8*time.Second),
	}
}

func (c *Config) IsDev() bool {
	return c.Environment == "dev"
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// secretOrEmpty returns the env var value, treating "PLACEHOLDER" as empty.
// Terraform initializes secrets with "PLACEHOLDER" — this ensures the mock
// provider activates in dev when no real token has been set.
func secretOrEmpty(key string) string {
	v := os.Getenv(key)
	if v == "PLACEHOLDER" {
		return ""
	}
	return v
}

func parseDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
