package jobs

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

// goldenImageCloudInit is a stripped-down cloud-init that installs only
// static, user-independent infrastructure. Per-user config is injected
// later via the minimal cloud-init template when provisioning from the snapshot.
var goldenImageCloudInit = template.Must(template.New("golden").Parse(`#!/bin/bash
set -euo pipefail
exec > >(tee -a /var/log/golden-image-init.log) 2>&1

echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) GOLDEN_IMAGE_BUILD_STARTED"

# --- Swap (prevents OOM on small instances during image pull) ---
if [ ! -f /swapfile ]; then
    fallocate -l 2G /swapfile
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile
    echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi

# --- System setup ---
export DEBIAN_FRONTEND=noninteractive
for i in 1 2 3; do
    apt-get update -qq && break
    sleep 5
done
apt-get install -y -qq ca-certificates curl jq ufw

# --- Firewall: leave DISABLED in golden image ---
# UFW rules are set up by the per-user minimal cloud-init on first boot.
# If we enable UFW here, the snapshot boots locked out (no SSH, no ports).
ufw --force disable

# --- SSH: leave password auth ON for golden image ---
# The per-user cloud-init injects SSH keys and hardens SSH config.
# If we disable password auth here, the snapshot boots with no access.

# --- Install Docker from official repository ---
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
apt-get update -qq
apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin
systemctl enable docker
systemctl start docker

# --- Pull OpenClaw images ---
docker pull ghcr.io/openclaw/openclaw:{{.OpenClawImageTag}}
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
    ghcr.io/openclaw/openclaw:{{.OpenClawImageTag}} openclaw setup --build-sandbox 2>/dev/null || \
    docker pull ghcr.io/openclaw/openclaw-sandbox:bookworm-slim 2>/dev/null || true

# --- Create openclaw user (UID 1000) with Docker access ---
useradd -r -m -u 1000 -s /usr/sbin/nologin openclaw || true
usermod -aG docker openclaw

# --- Directory structure ---
mkdir -p /opt/openclaw/data/openclaw /opt/openclaw/data/gogcli
chown -R 1000:1000 /opt/openclaw/data

# --- Caddy reverse proxy binary ---
for i in 1 2 3; do
    curl -sL "https://caddyserver.com/api/download?os=linux&arch=amd64" -o /usr/local/bin/caddy && break
    sleep 3
done
chmod +x /usr/local/bin/caddy
mkdir -p /etc/caddy

# --- Caddy systemd service ---
cat > /etc/systemd/system/caddy.service <<'CADDYSVCEOF'
[Unit]
Description=Caddy reverse proxy
After=network.target

[Service]
ExecStart=/usr/local/bin/caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
CADDYSVCEOF

# --- OpenClaw stack systemd service ---
cat > /etc/systemd/system/openclaw-stack.service <<'SVCEOF'
[Unit]
Description=OpenClaw Agent Stack
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/openclaw
ExecStart=/usr/bin/docker compose up -d --remove-orphans
ExecStop=/usr/bin/docker compose down
Restart=on-failure
RestartSec=30

[Install]
WantedBy=multi-user.target
SVCEOF

# --- Heartbeat systemd units ---
cat > /etc/systemd/system/openclaw-heartbeat.service <<'HBSVCEOF'
[Unit]
Description=OpenClaw Heartbeat

[Service]
Type=oneshot
ExecStart=/opt/openclaw/heartbeat.sh
HBSVCEOF

cat > /etc/systemd/system/openclaw-heartbeat.timer <<'HBTEOF'
[Unit]
Description=OpenClaw Heartbeat Timer

[Timer]
OnBootSec=90s
OnUnitActiveSec=300s
AccuracySec=10s

[Install]
WantedBy=timers.target
HBTEOF

systemctl daemon-reload

# --- Clean up for snapshot ---
# Stop Docker to ensure clean snapshot
docker compose -f /opt/openclaw/docker-compose.yml down 2>/dev/null || true

# Clear cloud-init state so it re-runs with new user-data on next boot.
# Hetzner assigns a new instance-id on server creation from snapshot,
# which triggers cloud-init to treat it as a new instance.
cloud-init clean --logs 2>/dev/null || true
rm -rf /var/lib/cloud/instances /var/lib/cloud/data /var/lib/cloud/sem
rm -f /var/log/cloud-init*.log

# Remove machine-specific state
rm -f /etc/machine-id
rm -f /var/lib/dbus/machine-id
truncate -s 0 /etc/hostname

# Ensure cloud-init runs on next boot
systemctl enable cloud-init cloud-init-local cloud-config cloud-final 2>/dev/null || true

echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) GOLDEN_IMAGE_BUILD_COMPLETED"
# Write marker file for polling
touch /opt/openclaw/.golden-image-ready
`))

type GoldenImageBuilder struct {
	pool             *pgxpool.Pool
	registry         *provider.Registry
	logger           *slog.Logger
	openClawImageTag string
}

func NewGoldenImageBuilder(pool *pgxpool.Pool, registry *provider.Registry, logger *slog.Logger, openClawImageTag string) *GoldenImageBuilder {
	return &GoldenImageBuilder{
		pool:             pool,
		registry:         registry,
		logger:           logger,
		openClawImageTag: openClawImageTag,
	}
}

// Build creates a golden image by:
// 1. Spinning up a temp server with static cloud-init
// 2. Waiting for cloud-init to complete
// 3. Creating a snapshot
// 4. Activating the snapshot as the new golden image
// 5. Deleting the temp server
// 6. Cleaning up deprecated images
func (b *GoldenImageBuilder) Build(ctx context.Context) (*models.GoldenImage, error) {
	imageTag := resolveImageTag(ctx, b.pool, b.openClawImageTag)

	// Look up any available provider mapping to get server type and region.
	// Golden images are region-scoped; we build for the first available region.
	mapping, err := db.GetAnyAvailableProviderMapping(ctx, b.pool, models.PlanStandard)
	if err != nil {
		return nil, fmt.Errorf("get provider mapping: %w", err)
	}

	prov, err := b.registry.Get(mapping.Provider)
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}

	// Create golden image record
	img := &models.GoldenImage{
		ID:              uuid.New(),
		Provider:        mapping.Provider,
		Region:          mapping.ProviderRegion,
		ServerType:      mapping.ProviderServerType,
		ProviderImageID: "", // set after snapshot
		OpenClawVersion: imageTag,
		Status:          models.GoldenImageBuilding,
	}
	if err := db.CreateGoldenImage(ctx, b.pool, img); err != nil {
		return nil, fmt.Errorf("create golden image record: %w", err)
	}

	b.logger.Info("golden-image: starting build",
		"image_id", img.ID,
		"openclaw_version", imageTag,
		"provider", mapping.Provider,
		"region", mapping.ProviderRegion,
	)

	// Render the static cloud-init
	var buf bytes.Buffer
	if err := goldenImageCloudInit.Execute(&buf, struct {
		OpenClawImageTag string
	}{
		OpenClawImageTag: imageTag,
	}); err != nil {
		return nil, fmt.Errorf("render golden image cloud-init: %w", err)
	}

	// Create temp server
	serverName := fmt.Sprintf("golden-image-%s", img.ID.String()[:8])
	server, err := prov.CreateServer(ctx, provider.CreateServerRequest{
		Name:       serverName,
		ServerType: mapping.ProviderServerType,
		Region:     mapping.ProviderRegion,
		Image:      mapping.ProviderImage,
		UserData:   buf.String(),
		Labels: map[string]string{
			"purpose":        "golden-image-build",
			"golden_image_id": img.ID.String(),
		},
	})
	if err != nil {
		_ = db.UpdateGoldenImageStatus(ctx, b.pool, img.ID, models.GoldenImageDeleted)
		return nil, fmt.Errorf("create temp server: %w", err)
	}

	b.logger.Info("golden-image: temp server created",
		"image_id", img.ID,
		"server_id", server.ProviderServerID,
	)

	// Ensure cleanup of temp server
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if delErr := prov.DeleteServer(cleanupCtx, server.ProviderServerID); delErr != nil {
			b.logger.Error("golden-image: failed to delete temp server",
				"server_id", server.ProviderServerID, "error", delErr)
		} else {
			b.logger.Info("golden-image: temp server deleted", "server_id", server.ProviderServerID)
		}
	}()

	// Wait for server to be running
	if err := b.waitServerReady(ctx, prov, server.ProviderServerID); err != nil {
		_ = db.UpdateGoldenImageStatus(ctx, b.pool, img.ID, models.GoldenImageDeleted)
		return nil, fmt.Errorf("wait server ready: %w", err)
	}

	// Wait for cloud-init to complete (poll health or use timeout)
	// Cloud-init installs Docker + pulls images, typically takes 5-7 minutes
	b.logger.Info("golden-image: waiting for cloud-init to complete", "image_id", img.ID)
	if err := b.waitCloudInitComplete(ctx, prov, server); err != nil {
		_ = db.UpdateGoldenImageStatus(ctx, b.pool, img.ID, models.GoldenImageDeleted)
		return nil, fmt.Errorf("wait cloud-init: %w", err)
	}

	// Stop the server before snapshotting for clean disk state
	b.logger.Info("golden-image: stopping server for snapshot", "image_id", img.ID)
	if err := prov.StopServer(ctx, server.ProviderServerID); err != nil {
		_ = db.UpdateGoldenImageStatus(ctx, b.pool, img.ID, models.GoldenImageDeleted)
		return nil, fmt.Errorf("stop server: %w", err)
	}

	// Wait for server to be off
	if err := b.waitServerOff(ctx, prov, server.ProviderServerID); err != nil {
		_ = db.UpdateGoldenImageStatus(ctx, b.pool, img.ID, models.GoldenImageDeleted)
		return nil, fmt.Errorf("wait server off: %w", err)
	}

	// Create snapshot
	b.logger.Info("golden-image: creating snapshot", "image_id", img.ID)
	snapshot, err := prov.CreateSnapshot(ctx, server.ProviderServerID, fmt.Sprintf("golden-image-%s", img.ID.String()[:8]))
	if err != nil {
		_ = db.UpdateGoldenImageStatus(ctx, b.pool, img.ID, models.GoldenImageDeleted)
		return nil, fmt.Errorf("create snapshot: %w", err)
	}

	// Update golden image with provider image ID
	img.ProviderImageID = snapshot.ProviderImageID
	if err := db.UpdateGoldenImageProviderID(ctx, b.pool, img.ID, snapshot.ProviderImageID); err != nil {
		return nil, fmt.Errorf("update provider image id: %w", err)
	}

	// Activate the golden image (deprecates previous)
	if err := db.ActivateGoldenImage(ctx, b.pool, img.ID); err != nil {
		return nil, fmt.Errorf("activate golden image: %w", err)
	}
	img.Status = models.GoldenImageActive

	b.logger.Info("golden-image: build complete",
		"image_id", img.ID,
		"provider_image_id", snapshot.ProviderImageID,
		"openclaw_version", imageTag,
	)

	// Clean up deprecated images in the background
	go b.cleanupDeprecatedImages()

	return img, nil
}

func (b *GoldenImageBuilder) waitServerReady(ctx context.Context, prov provider.InfraProvider, serverID string) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for server to be running")
		case <-ticker.C:
			server, err := prov.GetServer(ctx, serverID)
			if err != nil {
				continue
			}
			if server.Status == "running" {
				return nil
			}
		}
	}
}

func (b *GoldenImageBuilder) waitServerOff(ctx context.Context, prov provider.InfraProvider, serverID string) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(3 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for server to stop")
		case <-ticker.C:
			server, err := prov.GetServer(ctx, serverID)
			if err != nil {
				continue
			}
			if server.Status == "off" {
				return nil
			}
		}
	}
}

func (b *GoldenImageBuilder) waitCloudInitComplete(ctx context.Context, prov provider.InfraProvider, server *provider.Server) error {
	// Cloud-init typically takes 5-7 minutes. We poll for 10 minutes.
	// Since we can't SSH in easily from Go, we use a generous timeout.
	// The golden image cloud-init writes a marker file at the end.
	// For simplicity, we wait a fixed duration then verify the server is still running.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	timeout := time.After(10 * time.Minute)
	elapsed := time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			// If we got here, the server has been running for 10 min.
			// Cloud-init should be done. Verify server is still up.
			s, err := prov.GetServer(ctx, server.ProviderServerID)
			if err != nil {
				return fmt.Errorf("verify server after timeout: %w", err)
			}
			if s.Status != "running" {
				return fmt.Errorf("server not running after cloud-init timeout: status=%s", s.Status)
			}
			return nil
		case <-ticker.C:
			elapsed += 30 * time.Second
			s, err := prov.GetServer(ctx, server.ProviderServerID)
			if err != nil {
				b.logger.Debug("golden-image: poll failed", "error", err)
				continue
			}
			if s.Status != "running" {
				return fmt.Errorf("server died during cloud-init: status=%s", s.Status)
			}
			b.logger.Debug("golden-image: waiting for cloud-init", "elapsed", elapsed)

			// After minimum 6 minutes, assume cloud-init is done
			// (apt+docker+pull typically takes 4-6 minutes)
			if elapsed >= 6*time.Minute {
				b.logger.Info("golden-image: cloud-init wait complete", "elapsed", elapsed)
				return nil
			}
		}
	}
}

func (b *GoldenImageBuilder) cleanupDeprecatedImages() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	deprecated, err := db.GetDeprecatedGoldenImages(ctx, b.pool, 1*time.Hour)
	if err != nil {
		b.logger.Error("golden-image: failed to get deprecated images", "error", err)
		return
	}

	for _, img := range deprecated {
		prov, err := b.registry.Get(img.Provider)
		if err != nil {
			b.logger.Error("golden-image: provider not found for cleanup", "provider", img.Provider)
			continue
		}

		if err := prov.DeleteSnapshot(ctx, img.ProviderImageID); err != nil {
			b.logger.Error("golden-image: failed to delete deprecated snapshot",
				"image_id", img.ID, "provider_image_id", img.ProviderImageID, "error", err)
			continue
		}

		if err := db.UpdateGoldenImageStatus(ctx, b.pool, img.ID, models.GoldenImageDeleted); err != nil {
			b.logger.Error("golden-image: failed to mark as deleted", "image_id", img.ID, "error", err)
		}

		b.logger.Info("golden-image: deprecated image cleaned up", "image_id", img.ID)
	}
}
