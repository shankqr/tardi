package scripts

import (
	"strings"
	"testing"
)

func TestHeartbeatScript_NotEmpty(t *testing.T) {
	if HeartbeatScript == "" {
		t.Fatal("HeartbeatScript should not be empty")
	}
}

func TestHeartbeatScript_StartsWithShebang(t *testing.T) {
	if !strings.HasPrefix(HeartbeatScript, "#!/bin/bash") {
		t.Errorf("HeartbeatScript should start with #!/bin/bash, got: %q", HeartbeatScript[:30])
	}
}

func TestHeartbeatScript_SourcesEnvFile(t *testing.T) {
	if !strings.Contains(HeartbeatScript, "source /opt/openclaw/.env") {
		t.Error("HeartbeatScript should source /opt/openclaw/.env")
	}
}

func TestHeartbeatScript_ContainsHeartbeatEndpoint(t *testing.T) {
	if !strings.Contains(HeartbeatScript, "heartbeat") {
		t.Error("HeartbeatScript should contain heartbeat endpoint reference")
	}
}

func TestHeartbeatScript_ContainsSSHDriftGuard(t *testing.T) {
	if !strings.Contains(HeartbeatScript, "PasswordAuthentication") {
		t.Error("HeartbeatScript should contain SSH drift guard for PasswordAuthentication")
	}
}

func TestHeartbeatScript_ContainsMigrationLogic(t *testing.T) {
	if !strings.Contains(HeartbeatScript, "openclaw-net") {
		t.Error("HeartbeatScript should contain migration logic for old 2-container setup")
	}
}
