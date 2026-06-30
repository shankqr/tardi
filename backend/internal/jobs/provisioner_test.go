package jobs

import (
	"strings"
	"testing"
)

func TestBuildServerName(t *testing.T) {
	tests := []struct {
		name      string
		framework string
		email     string
		uniqueID  string
		want      string
	}{
		{
			name:      "normal openclaw",
			framework: "openclaw",
			email:     "shankqr@gmail.com",
			uniqueID:  "2cacd355",
			want:      "oc-shankqr.gmail.com-2cacd355",
		},
		{
			name:      "unknown framework uses full name",
			framework: "newagent",
			email:     "user@example.com",
			uniqueID:  "abcd1234",
			want:      "newagent-user.example.com-abcd1234",
		},
		{
			name:      "email with special characters",
			framework: "openclaw",
			email:     "user+tag@example.com",
			uniqueID:  "12345678",
			want:      "oc-user-tag.example.com-12345678",
		},
		{
			name:      "long email truncated to fit 63 chars",
			framework: "openclaw",
			email:     "verylongemailaddressthatexceedsthelimit@reallylongdomainname.example.com",
			uniqueID:  "abcd1234",
			want:      "oc-verylongemailaddressthatexceedsthelimit.reallylongd-abcd1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildServerName(tt.framework, tt.email, tt.uniqueID)
			if len(got) > 63 {
				t.Errorf("buildServerName() length = %d, exceeds 63 chars: %q", len(got), got)
			}
			if got != tt.want {
				t.Errorf("buildServerName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildUpgradeServerNameIsUniqueFromBaseName(t *testing.T) {
	base := buildServerName("hermes", "clawmyway@gmail.com", "f59c3841")
	got := buildUpgradeServerName("hermes", "clawmyway@gmail.com", "f59c3841", "abc12345")

	if got == base {
		t.Fatalf("buildUpgradeServerName() = base name %q", got)
	}
	if len(got) > 63 {
		t.Fatalf("buildUpgradeServerName() length = %d, exceeds 63 chars: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "-f59c3841-uabc12345") {
		t.Fatalf("buildUpgradeServerName() = %q, want upgrade suffix", got)
	}
}

func TestBuildUpgradeServerNameTruncatesLongEmail(t *testing.T) {
	got := buildUpgradeServerName(
		"openclaw",
		"verylongemailaddressthatexceedsthelimit@reallylongdomainname.example.com",
		"abcd1234",
		"deadbeef",
	)

	if len(got) > 63 {
		t.Fatalf("buildUpgradeServerName() length = %d, exceeds 63 chars: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "-abcd1234-udeadbeef") {
		t.Fatalf("buildUpgradeServerName() = %q, want upgrade suffix", got)
	}
}

func TestFrameworkCode(t *testing.T) {
	if got := frameworkCode("openclaw"); got != "oc" {
		t.Errorf("frameworkCode(\"openclaw\") = %q, want \"oc\"", got)
	}
	if got := frameworkCode("unknown"); got != "unknown" {
		t.Errorf("frameworkCode(\"unknown\") = %q, want \"unknown\"", got)
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"user@example.com", "user.example.com"},
		{"hello world!", "hello-world"},
		{"@start", "start"},
	}
	for _, tt := range tests {
		got := SanitizeLabelValue(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeLabelValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderCloudInitIncludesHostAdminHelper(t *testing.T) {
	got, err := RenderCloudInit(CloudInitData{
		AgentToken:        "agent-token",
		APIURL:            "https://api.example.com",
		InstanceID:        "instance-id",
		OpenClawAuthToken: "openclaw-token",
		OpenClawImageTag:  "latest",
	})
	if err != nil {
		t.Fatalf("RenderCloudInit() error = %v", err)
	}

	required := []string{
		"/api/agent/host-admin-script",
		"OPENCLAW_GID=${OPENCLAW_GID}",
		"TARDI_HOST_ADMIN_SOCKET=/run/tardi-host-admin/admin.sock",
		"TARDI_HOST_EXEC_TIMEOUT=1800",
		"PATH=/opt/tardi/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"https://dl.google.com/linux/chrome/deb/",
		"apt-get install -y -qq google-chrome-stable",
		"DESKTOP_APPS_INSTALLED",
		`- "${OPENCLAW_GID}"`,
		"/run/tardi-host-admin:/run/tardi-host-admin:rw",
		"/opt/openclaw/host-admin/bin:/opt/tardi/bin:ro",
		"/opt/openclaw/host-admin/bin/tardi-host-admin:/usr/local/bin/tardi-host-admin:ro",
		"/opt/openclaw/host-admin/bin/sudo:/usr/local/bin/sudo:ro",
		"/opt/openclaw/host-admin/bin/sudo:/usr/bin/sudo:ro",
		"Wants=tardi-host-admin.service",
	}
	for _, want := range required {
		if !strings.Contains(got, want) {
			t.Errorf("rendered cloud-init missing %q", want)
		}
	}
	if strings.Contains(got, "google-chrome-stable tradingview") {
		t.Error("rendered cloud-init should not eagerly install TradingView with Chrome")
	}
}

func TestRenderHermesCloudInitUsesDockerStack(t *testing.T) {
	got, err := RenderHermesCloudInit(HermesCloudInitData{
		AgentToken:       "agent-token",
		APIURL:           "https://api.example.com",
		InstanceID:       "instance-id",
		APIServerKey:     "api-server-key",
		HermesImageTag:   "latest",
		Provider:         "openrouter",
		Model:            "anthropic/claude-sonnet-4",
		TelegramBotToken: "telegram-token",
	})
	if err != nil {
		t.Fatalf("RenderHermesCloudInit() error = %v", err)
	}

	required := []string{
		"image: mirror.gcr.io/nousresearch/hermes-agent:latest",
		"container_name: hermes-agent",
		"network_mode: host",
		"./data:/opt/data:rw",
		"./data:/opt/hermes/data:rw",
		"/var/run/docker.sock:/var/run/docker.sock",
		"/opt/hermes/docker-cli/bin/docker:/usr/local/bin/docker:ro",
		"/opt/hermes/docker-cli/bin/docker-compose:/usr/local/bin/docker-compose:ro",
		"/opt/hermes/docker-cli/cli-plugins:/usr/local/lib/docker/cli-plugins:ro",
		"/api/agent/host-admin-script",
		"/run/tardi-host-admin:/run/tardi-host-admin:rw",
		"/opt/hermes/host-admin/bin:/opt/tardi/bin:ro",
		"/opt/hermes/host-admin/bin/tardi-host-admin:/usr/local/bin/tardi-host-admin:ro",
		"/opt/hermes/host-admin/bin/sudo:/usr/local/bin/sudo:ro",
		"/opt/hermes/host-admin/bin/sudo:/usr/bin/sudo:ro",
		"command: gateway run",
		"HERMES_DASHBOARD=1",
		"HERMES_DASHBOARD_HOST=127.0.0.1",
		"TELEGRAM_BOT_TOKEN=telegram-token",
		"GATEWAY_ALLOW_ALL_USERS=true",
		"TERMINAL_ENV=local",
		"DOCKER_HOST=unix:///var/run/docker.sock",
		"HERMES_DOCKER_BINARY=/usr/local/bin/docker",
		"TARDI_HOST_ADMIN_SOCKET=/run/tardi-host-admin/admin.sock",
		"Wants=tardi-host-admin.service",
		"hermes-stack.service",
		"https://dl.google.com/linux/chrome/deb/",
		"apt-get install -y -qq google-chrome-stable",
		"DESKTOP_APPS_INSTALLED",
		"docker compose up -d --remove-orphans",
		"model:\n  provider: \"openrouter\"\n  model: \"anthropic/claude-sonnet-4\"\nterminal:\n  backend: local\n  cwd: \"/opt/hermes/data/workspace\"",
	}
	for _, want := range required {
		if !strings.Contains(got, want) {
			t.Errorf("rendered Hermes cloud-init missing %q", want)
		}
	}
	if strings.Contains(got, "google-chrome-stable tradingview") {
		t.Error("rendered Hermes cloud-init should not eagerly install TradingView with Chrome")
	}

	forbidden := []string{
		"raw.githubusercontent.com/NousResearch/hermes-agent",
		"hermes-agent.service",
		"hermes-dashboard.service",
		"hermes config set",
	}
	for _, bad := range forbidden {
		if strings.Contains(got, bad) {
			t.Errorf("rendered Hermes cloud-init should not contain native install path %q", bad)
		}
	}
}
