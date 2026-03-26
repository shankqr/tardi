package sshexec

import (
	"strings"
	"testing"
	"time"
)

func TestRunCommand_NoAuthMethods(t *testing.T) {
	_, err := RunCommand("localhost", nil, "", "echo hello", 5*time.Second)
	if err == nil {
		t.Fatal("expected error when no auth methods are provided")
	}
	if !strings.Contains(err.Error(), "no auth methods available") {
		t.Errorf("error = %q, expected 'no auth methods available'", err.Error())
	}
}

func TestRunCommand_InvalidPrivateKey(t *testing.T) {
	// With an invalid private key and no password, it should fail with no auth methods
	_, err := RunCommand("localhost", []byte("not-a-key"), "", "echo hello", 5*time.Second)
	if err == nil {
		t.Fatal("expected error with invalid key and no password")
	}
	if !strings.Contains(err.Error(), "no auth methods available") {
		t.Errorf("error = %q, expected 'no auth methods available'", err.Error())
	}
}

func TestRunCommand_InvalidPrivateKeyWithPasswordFallback(t *testing.T) {
	// Invalid key but with password should attempt to connect (and fail since no SSH server).
	// Use 1s timeout since 192.0.2.1 (TEST-NET) is unreachable.
	_, err := RunCommand("192.0.2.1", []byte("not-a-key"), "password123", "echo hello", 1*time.Second)
	if err == nil {
		t.Fatal("expected error connecting to non-existent host")
	}
	// Should get a dial error, not an "no auth methods" error
	if strings.Contains(err.Error(), "no auth methods available") {
		t.Errorf("should have fallen back to password auth, got: %q", err.Error())
	}
}
