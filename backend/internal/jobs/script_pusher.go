package jobs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/scripts"
	"github.com/shanq/tardi/internal/sshexec"
)

// Advisory lock ID for script pusher (arbitrary fixed constant).
const scriptPusherLockID int64 = 0x7461726469_6862 // "tardi_hb"

// ScriptPusher pushes updated heartbeat scripts to all active VPSes on deploy.
// It runs once on startup: computes a hash of the current heartbeat and helper
// scripts, compares against the last-deployed hash in platform_settings, and if
// changed, SSHes into all active VPSes to overwrite the framework heartbeat.
type ScriptPusher struct {
	pool          *pgxpool.Pool
	logger        *slog.Logger
	sshPrivateKey []byte // Ed25519 private key for SSH auth (nil = password-only)
	sshPublicKey  string // "ssh-ed25519 AAAA..." for authorized_keys injection
}

func NewScriptPusher(pool *pgxpool.Pool, logger *slog.Logger, sshPrivateKey []byte, sshPublicKey string) *ScriptPusher {
	return &ScriptPusher{pool: pool, logger: logger, sshPrivateKey: sshPrivateKey, sshPublicKey: sshPublicKey}
}

// Start waits briefly for the HTTP server to be ready, then checks and pushes
// the heartbeat script if it has changed. Runs once and returns.
func (sp *ScriptPusher) Start(ctx context.Context) {
	// Let HTTP server start first (Cloud Run health checks).
	select {
	case <-time.After(10 * time.Second):
	case <-ctx.Done():
		return
	}

	sp.push(ctx)
}

func (sp *ScriptPusher) push(ctx context.Context) {
	currentHashInput := scripts.HeartbeatScript + "\n" + scripts.HermesHeartbeatScript + "\n" + scripts.HostAdminInstallScript
	currentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(currentHashInput)))

	// Acquire a dedicated connection so the advisory lock stays held.
	conn, err := sp.pool.Acquire(ctx)
	if err != nil {
		sp.logger.Error("script-pusher: failed to acquire connection", "error", err)
		return
	}
	defer conn.Release()

	// Try advisory lock — if another instance is already pushing, skip.
	var locked bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", scriptPusherLockID).Scan(&locked)
	if err != nil {
		sp.logger.Error("script-pusher: advisory lock query failed", "error", err)
		return
	}
	if !locked {
		sp.logger.Info("script-pusher: another instance is handling it, skipping")
		return
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", scriptPusherLockID) //nolint:errcheck

	// Check if script has changed.
	storedHash, err := db.GetPlatformSetting(ctx, sp.pool, "heartbeat_script_hash")
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		sp.logger.Error("script-pusher: failed to read stored hash", "error", err)
		return
	}
	if storedHash == currentHash {
		sp.logger.Info("script-pusher: heartbeat script unchanged, skipping")
		return
	}

	// Fetch all active instances with SSH credentials.
	instances, err := db.GetActiveInstancesByStatus(ctx, sp.pool, models.VpsStatusActive)
	if err != nil {
		sp.logger.Error("script-pusher: failed to list instances", "error", err)
		return
	}

	// Filter to instances that have SSH credentials.
	var targets []models.VpsInstance
	for _, inst := range instances {
		if inst.IPv4 != nil && inst.RootPassword != nil {
			targets = append(targets, inst)
		}
	}

	sp.logger.Info("script-pusher: heartbeat script changed, pushing to VPSes",
		"instances", len(targets),
		"hash", currentHash[:12],
	)

	if len(targets) > 0 {
		sp.pushToInstances(ctx, targets)
	}

	// Update stored hash (even on partial failure — unreachable VPSes
	// will catch up on their next config sync).
	if err := db.UpsertPlatformSetting(ctx, sp.pool, "heartbeat_script_hash", currentHash); err != nil {
		sp.logger.Error("script-pusher: failed to update stored hash", "error", err)
	}
}

func (sp *ScriptPusher) pushToInstances(ctx context.Context, instances []models.VpsInstance) {
	sshKeyDriftGuard := ""
	if sp.sshPublicKey != "" {
		sshKeyDriftGuard = fmt.Sprintf(`
mkdir -p /root/.ssh && chmod 700 /root/.ssh
grep -qF %q /root/.ssh/authorized_keys 2>/dev/null || echo %q >> /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
if ! grep -q 'PasswordAuthentication no' /etc/ssh/sshd_config.d/60-tardi.conf 2>/dev/null; then
    mkdir -p /etc/ssh/sshd_config.d
    printf 'PasswordAuthentication no\nPubkeyAuthentication yes\nPermitRootLogin prohibit-password\n' > /etc/ssh/sshd_config.d/60-tardi.conf
    sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
    sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
    systemctl restart sshd 2>/dev/null || systemctl restart ssh 2>/dev/null || true
fi`, sp.sshPublicKey, sp.sshPublicKey)
	}

	sem := make(chan struct{}, 5) // max 5 concurrent SSH connections
	var wg sync.WaitGroup

	for _, inst := range instances {
		wg.Add(1)
		go func(inst models.VpsInstance) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			script := scripts.HeartbeatScript
			path := "/opt/openclaw/heartbeat.sh"
			service := "openclaw-heartbeat.service"
			if inst.Framework == models.FrameworkHermes {
				script = scripts.HermesHeartbeatScript
				path = "/opt/hermes/heartbeat.sh"
				service = "hermes-heartbeat.service"
			}

			cmd := fmt.Sprintf("mkdir -p %q\ncat > %q <<'HBEOF'\n%s\nHBEOF\nchmod +x %q",
				strings.TrimSuffix(path, "/heartbeat.sh"), path, script, path)
			cmd += sshKeyDriftGuard
			cmd += fmt.Sprintf(`
if command -v systemctl >/dev/null 2>&1; then
    systemctl start %s >/dev/null 2>&1 || true
else
    nohup /bin/bash %s >/tmp/tardi-heartbeat-refresh.log 2>&1 &
fi`, service, path)

			_, err := sshexec.RunCommand(*inst.IPv4, sp.sshPrivateKey, *inst.RootPassword, cmd, 30*time.Second)
			if err != nil {
				sp.logger.Warn("script-pusher: failed to push to instance",
					"instance_id", inst.ID,
					"ip", *inst.IPv4,
					"error", err,
				)
				return
			}
			sp.logger.Info("script-pusher: pushed heartbeat script",
				"instance_id", inst.ID,
				"ip", *inst.IPv4,
			)
		}(inst)
	}

	wg.Wait()
}
