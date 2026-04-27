package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestExtractDeviceCode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ANSI-escaped codex output",
			in: "Welcome to Codex [v\x1b[90m0.121.0\x1b[0m]\n" +
				"\n2. Enter this one-time code \x1b[90m(expires in 15 minutes)\x1b[0m\n" +
				"   \x1b[94mS6ID-T7U0T\x1b[0m\n",
			want: "S6ID-T7U0T",
		},
		{
			name: "plain output, four-four code",
			in:   "code: A1B2-CDEF",
			want: "A1B2-CDEF",
		},
		{
			name: "no code present",
			in:   "Welcome to Codex — follow these steps:",
			want: "",
		},
		{
			name: "lowercase not matched",
			in:   "code: abcd-12345",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractDeviceCode(tc.in); got != tc.want {
				t.Errorf("extractDeviceCode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCodexLinkState_RateLimitWindow(t *testing.T) {
	s := NewCodexLinkState()
	id := uuid.New()

	if _, ok := s.recentStart(id); ok {
		t.Fatal("expected no prior start on fresh state")
	}

	s.recordStart(id)
	since, ok := s.recentStart(id)
	if !ok {
		t.Fatal("expected recent start to be recorded")
	}
	if since > time.Second {
		t.Errorf("expected recent start to be <1s, got %s", since)
	}
}

func TestCodexLinkState_RestartMarkerAndClear(t *testing.T) {
	s := NewCodexLinkState()
	id := uuid.New()

	if s.restartInFlight(id) {
		t.Fatal("expected no restart in flight on fresh state")
	}

	// First call claims the slot.
	if !s.markRestart(id) {
		t.Fatal("first markRestart should claim the slot")
	}
	if !s.restartInFlight(id) {
		t.Fatal("restartInFlight should be true after marking")
	}

	// Second call should be rejected as already in flight.
	if s.markRestart(id) {
		t.Fatal("second markRestart should return false while in flight")
	}

	// After clear, a fresh mark should succeed again.
	s.clearRestart(id)
	if s.restartInFlight(id) {
		t.Fatal("restartInFlight should be false after clear")
	}
	if !s.markRestart(id) {
		t.Fatal("markRestart should succeed again after clear")
	}
}

func TestCodexLinkState_RestartMarker_StaleRecycles(t *testing.T) {
	s := NewCodexLinkState()
	id := uuid.New()

	// Seed a stale marker directly to avoid a real sleep.
	s.restartAt.Store(id, time.Now().Add(-codexRestartMaxDuration-time.Second))

	// restartInFlight should recognise the marker as stale and remove it.
	if s.restartInFlight(id) {
		t.Fatal("stale marker should be cleared by restartInFlight")
	}
	// markRestart on a cleared slot should succeed.
	if !s.markRestart(id) {
		t.Fatal("markRestart should succeed after stale marker cleared")
	}
}
