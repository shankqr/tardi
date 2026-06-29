package scripts

import (
	"os"
	"os/exec"
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

func TestHeartbeatScript_CapturesOpenClawVersionSuffix(t *testing.T) {
	if !strings.Contains(HeartbeatScript, `[0-9]{4}\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?`) {
		t.Error("HeartbeatScript should preserve OpenClaw version suffixes like 2026.5.3-1")
	}
}

func TestHeartbeatScript_SelfUpdatesFromAPI(t *testing.T) {
	required := []string{
		"/api/agent/heartbeat-script",
		"CURRENT_SCRIPT=$(cat /opt/openclaw/heartbeat.sh",
		"exec /bin/bash /opt/openclaw/heartbeat.sh",
	}
	for _, want := range required {
		if !strings.Contains(HeartbeatScript, want) {
			t.Errorf("HeartbeatScript should contain self-update step %q", want)
		}
	}
}

func TestHeartbeatScript_ContainsSSHDriftGuard(t *testing.T) {
	if !strings.Contains(HeartbeatScript, "PasswordAuthentication") {
		t.Error("HeartbeatScript should contain SSH drift guard for PasswordAuthentication")
	}
}

func TestHeartbeatScript_ContainsHostAdminDriftGuard(t *testing.T) {
	required := []string{
		"/api/agent/host-admin-script",
		"OPENCLAW_GID",
		`- "${OPENCLAW_GID}"`,
		"/run/tardi-host-admin:/run/tardi-host-admin:rw",
		"/opt/openclaw/host-admin/bin:/opt/tardi/bin:ro",
		"/opt/openclaw/host-admin/bin/tardi-host-admin:/usr/local/bin/tardi-host-admin:ro",
		"/opt/openclaw/host-admin/bin/sudo:/usr/local/bin/sudo:ro",
		"/opt/openclaw/host-admin/bin/sudo:/usr/bin/sudo:ro",
		"TARDI_HOST_ADMIN_SOCKET=/run/tardi-host-admin/admin.sock",
		"TARDI_HOST_EXEC_TIMEOUT=1800",
		"PATH=/opt/tardi/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"tardi-host-admin-container-check.log",
	}
	for _, want := range required {
		if !strings.Contains(HeartbeatScript, want) {
			t.Errorf("HeartbeatScript should contain host admin setup %q", want)
		}
	}
}

func TestHeartbeatScript_ContainsChannelAndCodexDriftGuards(t *testing.T) {
	required := []string{
		"openclaw-channel-drift.log",
		"tg = channels",
		"dmPolicy",
		`"streaming", {"mode": "off"}`,
		"openclaw-codex-model-drift.log",
		"openclaw-codex-plugin-drift.log",
		"clawhub:@openclaw/codex",
		"codex-app-server",
		"openai-codex-responses",
		"codex_usage_limit_exceeded",
	}
	for _, want := range required {
		if !strings.Contains(HeartbeatScript, want) {
			t.Errorf("HeartbeatScript should contain %q", want)
		}
	}
}

func TestHeartbeatScript_Syntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	path := t.TempDir() + "/heartbeat.sh"
	if err := os.WriteFile(path, []byte(HeartbeatScript), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("HeartbeatScript failed bash -n: %v\n%s", err, out)
	}
}

func TestHermesHeartbeatScript_UsesDockerStackAndLatestUpdates(t *testing.T) {
	required := []string{
		"source /opt/hermes/.env",
		"CURRENT_SCRIPT=$(cat /opt/hermes/heartbeat.sh",
		"exec /bin/bash /opt/hermes/heartbeat.sh",
		"container_name: hermes-agent",
		"network_mode: host",
		"mirror.gcr.io/nousresearch/hermes-agent:latest",
		"./data:/opt/hermes/data:rw",
		"/opt/hermes/docker-cli/bin/docker:/usr/local/bin/docker:ro",
		"/opt/hermes/docker-cli/bin/docker-compose:/usr/local/bin/docker-compose:ro",
		"/opt/hermes/docker-cli/cli-plugins:/usr/local/lib/docker/cli-plugins:ro",
		"/api/agent/host-admin-script",
		"/run/tardi-host-admin:/run/tardi-host-admin:rw",
		"/opt/hermes/host-admin/bin:/opt/tardi/bin:ro",
		"/opt/hermes/host-admin/bin/tardi-host-admin:/usr/local/bin/tardi-host-admin:ro",
		"/opt/hermes/host-admin/bin/sudo:/usr/local/bin/sudo:ro",
		"/opt/hermes/host-admin/bin/sudo:/usr/bin/sudo:ro",
		"TARDI_HOST_ADMIN_SOCKET=/run/tardi-host-admin/admin.sock",
		"TERMINAL_ENV=local",
		"DOCKER_HOST=unix:///var/run/docker.sock",
		"HERMES_DOCKER_BINARY=/usr/local/bin/docker",
		"backend: local",
		`cwd: "/opt/hermes/data/workspace"`,
		"importlib.metadata",
		`"hermes-agent"`,
		`model: "${NEW_MODEL}"`,
		`HERMES_DEFAULT_MODEL="anthropic/claude-sonnet-4.6"`,
		`"openai-codex"`,
		`codex_reauth_required`,
		`codex_auth_present`,
		`usage_limit_reached`,
		`TELEGRAM_BOT_TOKEN`,
		`GATEWAY_ALLOW_ALL_USERS=true`,
		`dashboard_token`,
		`API_SERVER_KEY=${API_DASHBOARD_TOKEN}`,
		`tardi-dashboard-shim.service`,
		"https://api.github.com/repos/NousResearch/hermes-agent/releases/latest",
		"resolve_latest_hermes_release",
		"RESOLVED_TARGET_VERSION=$(resolve_latest_hermes_release)",
		"HERMES_IMAGE_REPO=\"mirror.gcr.io/nousresearch/hermes-agent\"",
		"set_hermes_image_ref",
		"${HERMES_IMAGE_REPO}:${RESOLVED_TARGET_VERSION}",
		"find /opt/hermes/.update_status -mmin +30",
		"docker compose -f /opt/hermes/docker-compose.yml pull hermes-agent",
		"docker compose -f /opt/hermes/docker-compose.yml up -d hermes-agent",
		`[ "$TARGET_VERSION" = "latest" ]`,
		"hermes-stack.service",
	}
	for _, want := range required {
		if !strings.Contains(HermesHeartbeatScript, want) {
			t.Errorf("HermesHeartbeatScript should contain %q", want)
		}
	}

	forbidden := []string{
		"raw.githubusercontent.com/NousResearch/hermes-agent",
		"systemctl restart hermes-agent",
		"su - hermes -c",
		`[ "$TARGET_VERSION" != "$CURRENT_TAG" ]`,
	}
	for _, bad := range forbidden {
		if strings.Contains(HermesHeartbeatScript, bad) {
			t.Errorf("HermesHeartbeatScript should not contain native install/update path %q", bad)
		}
	}
}

func TestHermesHeartbeatScript_Syntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	path := t.TempDir() + "/heartbeat-hermes.sh"
	if err := os.WriteFile(path, []byte(HermesHeartbeatScript), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("HermesHeartbeatScript failed bash -n: %v\n%s", err, out)
	}
}

func TestHostAdminInstallScript_ExposesDesktopActionsAndRootBridge(t *testing.T) {
	required := []string{
		"desktop.install",
		"desktop.start",
		"desktop.open",
		"host.exec",
		"MAX_EXEC_SECONDS",
		"TARDI_HOST_ADMIN_SUDO_VERSION=20260429",
		"TARDI_HOST_ADMIN_BIN",
		"Unix socket unavailable",
		"cat > \"$BIN_DIR/sudo\"",
		"exec \"$CLIENT\" host.exec",
		"tardi-host-admin.service",
		"tardi-desktop.service",
		"-SecurityTypes None",
		"TradingView launch requested",
	}
	for _, want := range required {
		if !strings.Contains(HostAdminInstallScript, want) {
			t.Errorf("HostAdminInstallScript should contain %q", want)
		}
	}
}

func TestHostAdminInstallScript_Syntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	path := t.TempDir() + "/install-host-admin.sh"
	if err := os.WriteFile(path, []byte(HostAdminInstallScript), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("HostAdminInstallScript failed bash -n: %v\n%s", err, out)
	}
}

func TestHostAdminPython_Syntax(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	server, ok := extractHostAdminHeredoc("cat > \"$INSTALL_DIR/server.py\" <<'PYEOF'\n", "\nPYEOF\n")
	if !ok {
		t.Fatal("server.py heredoc not found")
	}
	path := t.TempDir() + "/server.py"
	if err := os.WriteFile(path, []byte(server), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("python3", "-c", "import ast, pathlib, sys; ast.parse(pathlib.Path(sys.argv[1]).read_text())", path).CombinedOutput()
	if err != nil {
		t.Fatalf("host admin server.py failed syntax parse: %v\n%s", err, out)
	}
}

func TestHostAdminClientScripts_Syntax(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	tests := []struct {
		name  string
		start string
		end   string
	}{
		{
			name:  "tardi-host-admin",
			start: "cat > \"$BIN_DIR/tardi-host-admin\" <<'CLIENTEOF'\n",
			end:   "\nCLIENTEOF\n",
		},
		{
			name:  "sudo",
			start: "cat > \"$BIN_DIR/sudo\" <<'SUDOEOF'\n",
			end:   "\nSUDOEOF\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, ok := extractHostAdminHeredoc(tt.start, tt.end)
			if !ok {
				t.Fatalf("%s heredoc not found", tt.name)
			}
			path := t.TempDir() + "/" + tt.name
			if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("sh", "-n", path).CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed sh -n: %v\n%s", tt.name, err, out)
			}
		})
	}
}

func extractHostAdminHeredoc(start, end string) (string, bool) {
	i := strings.Index(HostAdminInstallScript, start)
	if i < 0 {
		return "", false
	}
	i += len(start)
	j := strings.Index(HostAdminInstallScript[i:], end)
	if j < 0 {
		return "", false
	}
	return HostAdminInstallScript[i : i+j], true
}
