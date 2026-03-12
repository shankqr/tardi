package sshexec

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// RunCommand connects to the host via SSH and executes the given command.
// Returns combined stdout+stderr output and any error.
func RunCommand(host, password, command string, timeout time.Duration) (string, error) {
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
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
