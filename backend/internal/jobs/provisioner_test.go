package jobs

import (
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
