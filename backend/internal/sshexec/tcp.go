package sshexec

import (
	"fmt"
	"net"
)

type tunneledConn struct {
	net.Conn
	clientClose func() error
}

func (c *tunneledConn) Close() error {
	connErr := c.Conn.Close()
	clientErr := c.clientClose()
	if connErr != nil {
		return connErr
	}
	return clientErr
}

// DialTCP opens a TCP connection from the VPS host to target over SSH.
// Target is resolved on the VPS, so "127.0.0.1:5901" reaches a private
// service bound to loopback on the user's instance.
func DialTCP(host string, privateKey []byte, password, target string) (net.Conn, error) {
	client, err := dial(host, privateKey, password)
	if err != nil {
		return nil, err
	}

	conn, err := client.Dial("tcp", target)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ssh tcp dial %s: %w", target, err)
	}

	return &tunneledConn{
		Conn:        conn,
		clientClose: client.Close,
	}, nil
}
