package api

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestScopedTicketRequiresMatchingScope(t *testing.T) {
	secret := []byte("desktop-ticket-secret")
	userID := uuid.New()
	instanceID := uuid.New()

	ticket := signScopedTicket(secret, "desktop", userID, instanceID, time.Now().Add(time.Minute))

	gotUser, gotInstance, err := verifyScopedTicket(secret, "desktop", ticket)
	if err != nil {
		t.Fatalf("verifyScopedTicket() error = %v", err)
	}
	if gotUser != userID || gotInstance != instanceID {
		t.Fatalf("verifyScopedTicket() = %s/%s, want %s/%s", gotUser, gotInstance, userID, instanceID)
	}

	if _, _, err := verifyScopedTicket(secret, "terminal", ticket); err == nil {
		t.Fatal("verifyScopedTicket() accepted ticket with wrong scope")
	}
}

func TestScopedTicketRejectsTampering(t *testing.T) {
	secret := []byte("desktop-ticket-secret")
	ticket := signScopedTicket(secret, "desktop", uuid.New(), uuid.New(), time.Now().Add(time.Minute))
	tampered := strings.Replace(ticket, "desktop", "desktoz", 1)

	if _, _, err := verifyScopedTicket(secret, "desktop", tampered); err == nil {
		t.Fatal("verifyScopedTicket() accepted tampered ticket")
	}
}

func TestBuildDesktopPrepareCommand(t *testing.T) {
	cmd := buildDesktopPrepareCommand(true, "NASDAQ:AAPL")

	required := []string{
		"/api/agent/host-admin-script",
		"/opt/openclaw/host-admin/bin/tardi-host-admin",
		"/opt/hermes/host-admin/bin/tardi-host-admin",
		"TARDI_HOST_ADMIN_CLIENT_VERSION=20260629",
		"desktop.install",
		"desktop.open 'NASDAQ:AAPL'",
		"-SecurityTypes None",
		"TARDI_DESKTOP_SERVICE_VERSION=20260629",
		"systemctl is-active --quiet tardi-desktop.service",
		"/dev/tcp/127.0.0.1/5901",
	}
	for _, want := range required {
		if !strings.Contains(cmd, want) {
			t.Fatalf("buildDesktopPrepareCommand() missing %q", want)
		}
	}
}

func TestBuildDesktopReadyCommand(t *testing.T) {
	cmd := buildDesktopReadyCommand()

	required := []string{
		"TARDI_DESKTOP_SERVICE_VERSION=20260629",
		"systemctl is-active --quiet tardi-desktop.service",
		"/dev/tcp/127.0.0.1/5901",
		"echo ready",
		"echo preparing",
	}
	for _, want := range required {
		if !strings.Contains(cmd, want) {
			t.Fatalf("buildDesktopReadyCommand() missing %q", want)
		}
	}
}

func TestBuildDesktopPrepareKickoffCommand(t *testing.T) {
	cmd := buildDesktopPrepareKickoffCommand(false, "")

	required := []string{
		"/tmp/tardi-desktop-prepare.sh",
		"TARDI_DESKTOP_PREPARE",
		"flock -n /tmp/tardi-desktop-prepare.lock",
		"nohup bash -lc",
		"desktop_prepare_started",
	}
	for _, want := range required {
		if !strings.Contains(cmd, want) {
			t.Fatalf("buildDesktopPrepareKickoffCommand() missing %q", want)
		}
	}
}
