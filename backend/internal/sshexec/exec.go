package sshexec

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// RunCommand connects to the host via SSH and executes the given command.
// It tries key-based auth first (if privateKey is non-nil), then falls back
// to password auth (if password is non-empty). During the migration from
// password to key-based auth, both may be provided.
func RunCommand(host string, privateKey []byte, password, command string, timeout time.Duration) (string, error) {
	var authMethods []ssh.AuthMethod

	if len(privateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			slog.Warn("sshexec: failed to parse private key, falling back to password", "error", err)
		} else {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	if len(authMethods) == 0 {
		return "", fmt.Errorf("ssh: no auth methods available (no key or password)")
	}

	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // VPS managed by us, no known_hosts
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(host, "22")
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("ssh dial: %w", err)
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
