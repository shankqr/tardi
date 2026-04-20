package sshexec

import (
	"strings"
	"testing"
	"time"
)

// TestDefaultRunner_DelegatesToRunCommand verifies DefaultRunner forwards
// arguments to the package-level RunCommand. We drive it into the fast
// "no auth methods available" path so the test exits immediately.
func TestDefaultRunner_DelegatesToRunCommand(t *testing.T) {
	_, err := DefaultRunner{}.RunCommand("localhost", nil, "", "echo hello", 1*time.Second)
	if err == nil {
		t.Fatal("expected error from DefaultRunner, got nil")
	}
	if !strings.Contains(err.Error(), "no auth methods available") {
		t.Errorf("error = %q, expected 'no auth methods available'", err.Error())
	}
}

// Compile-time assertion that DefaultRunner satisfies the Runner interface.
var _ Runner = DefaultRunner{}
