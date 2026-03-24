package config

import (
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Config struct {
	Port                string
	DatabaseURL         string
	AllowedOrigins      []string
	FirebaseProjectID   string
	StripeSecretKey     string
	StripeWebhookSecret string
	HetznerAPIToken     string
	CloudflareAPIToken  string
	CloudflareZoneID    string
	CloudflareBaseDomain string // e.g. "agents.tardi.ai" — instances get <id>.agents.tardi.ai
	Environment         string
	LogLevel            string
	APIURL              string
	OpenClawImageTag    string
	AdminAPIToken       string

	// Google OAuth (for delegated Google account access)
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string
	TokenEncryptionKey      string // 32-byte hex for AES-256-GCM token encryption

	// Firewall: comma-separated CIDRs for backend egress IPs (e.g. Cloud NAT static IPs).
	// When set, VPS UFW rules restrict SSH (22) and OpenClaw (18789) to these CIDRs
	// plus Cloudflare IP ranges. When empty, SSH remains open to all (less secure).
	BackendEgressCIDRs string

	// SSH key-based auth: base64-encoded Ed25519 private key for SSHing into VPSes.
	// The public key is derived at startup and injected into VPSes via cloud-init
	// and heartbeat script. When set, key auth is preferred over password auth.
	SSHPrivateKey []byte  // decoded PEM bytes
	SSHPublicKey  string  // "ssh-ed25519 AAAA..." format for authorized_keys

	// Mock provider delays (dev only)
	MockInitDelay      time.Duration
	MockHeartbeatDelay time.Duration
	MockStopDelay      time.Duration
	MockStartDelay     time.Duration
	MockRestartDelay   time.Duration
}

func Load() *Config {
	cfg := &Config{
		Port:               envOrDefault("PORT", "8080"),
		DatabaseURL:        envOrDefault("DATABASE_URL", "postgres://tardi:tardi@localhost:5432/tardi?sslmode=disable"),
		AllowedOrigins:     strings.Split(envOrDefault("ALLOWED_ORIGINS", "http://localhost:5173"), ","),
		FirebaseProjectID:  os.Getenv("FIREBASE_PROJECT_ID"),
		StripeSecretKey:    os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		HetznerAPIToken:    secretOrEmpty("HETZNER_API_TOKEN"),
		CloudflareAPIToken:  secretOrEmpty("CLOUDFLARE_API_TOKEN"),
		CloudflareZoneID:    os.Getenv("CLOUDFLARE_ZONE_ID"),
		CloudflareBaseDomain: os.Getenv("CLOUDFLARE_BASE_DOMAIN"),
		Environment:        envOrDefault("ENVIRONMENT", "dev"),
		LogLevel:           envOrDefault("LOG_LEVEL", "info"),
		APIURL:             envOrDefault("API_URL", "http://localhost:8080"),
		OpenClawImageTag:   envOrDefault("OPENCLAW_IMAGE_TAG", "latest"),
		AdminAPIToken:     os.Getenv("ADMIN_API_TOKEN"),

		GoogleOAuthClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret: secretOrEmpty("GOOGLE_OAUTH_CLIENT_SECRET"),
		TokenEncryptionKey:      secretOrEmpty("TOKEN_ENCRYPTION_KEY"),

		BackendEgressCIDRs: os.Getenv("BACKEND_EGRESS_CIDRS"),

		SSHPrivateKey: loadSSHPrivateKey(),

		MockInitDelay:      parseDuration("MOCK_INIT_DELAY", 12*time.Second),
		MockHeartbeatDelay: parseDuration("MOCK_HEARTBEAT_DELAY", 18*time.Second),
		MockStopDelay:      parseDuration("MOCK_STOP_DELAY", 3*time.Second),
		MockStartDelay:     parseDuration("MOCK_START_DELAY", 5*time.Second),
		MockRestartDelay:   parseDuration("MOCK_RESTART_DELAY", 8*time.Second),
	}

	// Derive public key from private key for injection into VPS authorized_keys
	if len(cfg.SSHPrivateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(cfg.SSHPrivateKey)
		if err != nil {
			slog.Error("config: failed to parse SSH private key", "error", err)
		} else {
			cfg.SSHPublicKey = string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
			cfg.SSHPublicKey = strings.TrimSpace(cfg.SSHPublicKey)
			slog.Info("config: SSH key loaded", "public_key_prefix", cfg.SSHPublicKey[:min(40, len(cfg.SSHPublicKey))]+"...")
		}
	}

	return cfg
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

// loadSSHPrivateKey reads SSH_PRIVATE_KEY env var (base64-encoded PEM) and
// returns the decoded bytes. Returns nil if not set or invalid.
func loadSSHPrivateKey() []byte {
	v := secretOrEmpty("SSH_PRIVATE_KEY")
	if v == "" {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		slog.Error("config: SSH_PRIVATE_KEY is not valid base64", "error", err)
		return nil
	}
	return key
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
