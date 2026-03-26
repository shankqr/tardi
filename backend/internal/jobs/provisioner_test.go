package jobs

import (
	"encoding/hex"
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

func TestGenerateAgentToken(t *testing.T) {
	t.Run("returns 64-character hex string", func(t *testing.T) {
		token, err := GenerateAgentToken()
		if err != nil {
			t.Fatalf("GenerateAgentToken() returned error: %v", err)
		}
		if len(token) != 64 {
			t.Errorf("GenerateAgentToken() length = %d, want 64", len(token))
		}
		// Verify it is valid hex
		decoded, err := hex.DecodeString(token)
		if err != nil {
			t.Errorf("GenerateAgentToken() returned invalid hex: %v", err)
		}
		if len(decoded) != 32 {
			t.Errorf("GenerateAgentToken() decoded length = %d, want 32 bytes", len(decoded))
		}
	})

	t.Run("two calls produce different tokens", func(t *testing.T) {
		token1, err := GenerateAgentToken()
		if err != nil {
			t.Fatalf("first GenerateAgentToken() returned error: %v", err)
		}
		token2, err := GenerateAgentToken()
		if err != nil {
			t.Fatalf("second GenerateAgentToken() returned error: %v", err)
		}
		if token1 == token2 {
			t.Errorf("GenerateAgentToken() produced identical tokens: %q", token1)
		}
	})
}

func TestGenerateRootPassword(t *testing.T) {
	t.Run("returns 32-character hex string", func(t *testing.T) {
		pw, err := GenerateRootPassword()
		if err != nil {
			t.Fatalf("GenerateRootPassword() returned error: %v", err)
		}
		if len(pw) != 32 {
			t.Errorf("GenerateRootPassword() length = %d, want 32", len(pw))
		}
		// Verify it is valid hex
		decoded, err := hex.DecodeString(pw)
		if err != nil {
			t.Errorf("GenerateRootPassword() returned invalid hex: %v", err)
		}
		if len(decoded) != 16 {
			t.Errorf("GenerateRootPassword() decoded length = %d, want 16 bytes", len(decoded))
		}
	})

	t.Run("two calls produce different passwords", func(t *testing.T) {
		pw1, err := GenerateRootPassword()
		if err != nil {
			t.Fatalf("first GenerateRootPassword() returned error: %v", err)
		}
		pw2, err := GenerateRootPassword()
		if err != nil {
			t.Fatalf("second GenerateRootPassword() returned error: %v", err)
		}
		if pw1 == pw2 {
			t.Errorf("GenerateRootPassword() produced identical passwords: %q", pw1)
		}
	})
}

func TestRenderCloudInit(t *testing.T) {
	t.Run("renders with all fields populated", func(t *testing.T) {
		data := CloudInitData{
			AgentToken:         "test-agent-token-abc123",
			APIURL:             "https://api.tardi.ai",
			InstanceID:         "inst-99887766",
			OpenRouterAPIKey:   "or-key-xxx",
			AnthropicAPIKey:    "ant-key-yyy",
			OpenAIAPIKey:       "oai-key-zzz",
			OpenClawAuthToken:  "oc-auth-token-456",
			OpenClawImageTag:   "v1.2.3",
			Provider:           "hetzner",
			Model:              "claude-sonnet-4-20250514",
			ConfigVersion:      3,
			RootPassword:       "rootpw123abc",
			SSHPublicKey:       "ssh-ed25519 AAAAC3test",
			TelegramBotToken:   "123456:ABC-DEF",
			Domain:             "agent.tardi.ai",
			PreviewDomain:      "preview.tardi.ai",
			BackendEgressCIDRs: "10.0.0.0/8",
			AllModelIDs:        []string{"model-a", "model-b"},
		}
		output, err := RenderCloudInit(data)
		if err != nil {
			t.Fatalf("RenderCloudInit() returned error: %v", err)
		}
		for _, want := range []string{
			data.AgentToken,
			data.APIURL,
			data.InstanceID,
		} {
			if !strings.Contains(output, want) {
				t.Errorf("RenderCloudInit() output missing %q", want)
			}
		}
	})

	t.Run("renders with minimal fields", func(t *testing.T) {
		data := CloudInitData{
			AgentToken: "minimal-token",
			APIURL:     "https://api.tardi.ai",
			InstanceID: "inst-minimal",
		}
		output, err := RenderCloudInit(data)
		if err != nil {
			t.Fatalf("RenderCloudInit() returned error: %v", err)
		}
		if output == "" {
			t.Error("RenderCloudInit() returned empty output")
		}
	})

	t.Run("output contains cloud-init marker", func(t *testing.T) {
		data := CloudInitData{
			AgentToken: "marker-token",
			APIURL:     "https://api.tardi.ai",
			InstanceID: "inst-marker",
		}
		output, err := RenderCloudInit(data)
		if err != nil {
			t.Fatalf("RenderCloudInit() returned error: %v", err)
		}
		if !strings.HasPrefix(output, "#!/bin/bash") {
			t.Errorf("RenderCloudInit() output does not start with #!/bin/bash, got prefix: %q", output[:min(len(output), 50)])
		}
	})
}
