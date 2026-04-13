package sshexec

import (
	"bytes"
	"fmt"
	"time"
)

// RunCommand connects to the host via SSH and executes the given command.
// It tries key-based auth first (if privateKey is non-nil), then falls back
// to password auth (if password is non-empty). During the migration from
// password to key-based auth, both may be provided.
func RunCommand(host string, privateKey []byte, password, command string, timeout time.Duration) (string, error) {
	client, err := dial(host, privateKey, password)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case err := <-done:
		if err != nil {
			return buf.String(), fmt.Errorf("command failed: %w (output: %s)", err, buf.String())
		}
		return buf.String(), nil
	case <-time.After(timeout):
		return buf.String(), fmt.Errorf("command timed out after %s", timeout)
	}
}
