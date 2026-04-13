package sshexec

import (
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// Shell is an interactive PTY-backed SSH session. Stdin accepts raw keystroke
// bytes and Stdout produces merged stdout+stderr bytes. Callers are responsible
// for calling Close when done.
type Shell struct {
	client  *ssh.Client
	session *ssh.Session
	Stdin   io.WriteCloser
	Stdout  io.Reader
}

// OpenShell dials the host over SSH, allocates an xterm-256color PTY of the
// given dimensions, and starts an interactive login shell.
func OpenShell(host string, privateKey []byte, password string, cols, rows int) (*Shell, error) {
	client, err := dial(host, privateKey, password)
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("ssh session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	// With a PTY allocated, the remote tty multiplexes stderr into stdout,
	// so we only need a single output pipe.
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	return &Shell{
		client:  client,
		session: session,
		Stdin:   stdin,
		Stdout:  stdout,
	}, nil
}

// Resize updates the PTY window size on the remote end.
func (s *Shell) Resize(cols, rows int) error {
	return s.session.WindowChange(rows, cols)
}

// Close terminates the SSH session and underlying client connection.
func (s *Shell) Close() error {
	_ = s.session.Close()
	return s.client.Close()
}

// dial establishes an SSH client connection to host:22 using key-based auth
// (preferred) and/or password auth fallback. Both RunCommand and OpenShell
// share this helper.
func dial(host string, privateKey []byte, password string) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod

	if len(privateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("ssh: no auth methods available (no key or password)")
	}

	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // VPS managed by us, no known_hosts
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	return client, nil
}

