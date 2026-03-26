package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractAgentToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		expect string
	}{
		{
			name:   "with Bearer prefix",
			header: "Bearer my-secret-token",
			expect: "my-secret-token",
		},
		{
			name:   "without Bearer prefix",
			header: "my-secret-token",
			expect: "",
		},
		{
			name:   "empty string",
			header: "",
			expect: "",
		},
		{
			name:   "Bearer with empty token",
			header: "Bearer ",
			expect: "",
		},
		{
			name:   "lowercase bearer (wrong case)",
			header: "bearer my-token",
			expect: "",
		},
		{
			name:   "Bearer with extra spaces in token",
			header: "Bearer token with spaces",
			expect: "token with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}

			got := extractAgentToken(r)
			if got != tt.expect {
				t.Errorf("extractAgentToken() = %q, want %q", got, tt.expect)
			}
		})
	}
}
