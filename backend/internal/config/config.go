package config

import (
	"os"
	"strings"
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
}

func Load() *Config {
	return &Config{
		Port:               envOrDefault("PORT", "8080"),
		DatabaseURL:        envOrDefault("DATABASE_URL", "postgres://tardi:tardi@localhost:5432/tardi?sslmode=disable"),
		AllowedOrigins:     strings.Split(envOrDefault("ALLOWED_ORIGINS", "http://localhost:5173"), ","),
		FirebaseProjectID:  os.Getenv("FIREBASE_PROJECT_ID"),
		StripeSecretKey:    os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		HetznerAPIToken:    os.Getenv("HETZNER_API_TOKEN"),
		Environment:        envOrDefault("ENVIRONMENT", "dev"),
		LogLevel:           envOrDefault("LOG_LEVEL", "info"),
		APIURL:             envOrDefault("API_URL", "http://localhost:8080"),
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
