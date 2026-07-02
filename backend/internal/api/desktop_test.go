package api

import (
	"os"
	"os/exec"
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
		"TARDI_HOST_ADMIN_CLIENT_VERSION=2026070301",
		"desktop.install",
		"desktop.open 'NASDAQ:AAPL'",
		"-SecurityTypes None",
		"-UseBlacklist 0",
		"TARDI_DESKTOP_SERVICE_VERSION=2026070301",
		"systemctl is-active --quiet tardi-desktop.service",
		"command -v google-chrome",
		"vnc_probe",
		"Too many security failures",
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
		"TARDI_DESKTOP_SERVICE_VERSION=2026070301",
		"systemctl is-active --quiet tardi-desktop.service",
		"command -v google-chrome",
		"vnc_probe",
		"systemctl restart tardi-desktop.service",
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

func TestBuildDesktopShellCommandsSyntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	commands := map[string]string{
		"ready":   buildDesktopReadyCommand(),
		"prepare": buildDesktopPrepareCommand(true, "NASDAQ:AAPL"),
		"kickoff": buildDesktopPrepareKickoffCommand(false, ""),
	}
	for name, command := range commands {
		path := t.TempDir() + "/" + name + ".sh"
		if err := os.WriteFile(path, []byte(command), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("bash", "-n", path).CombinedOutput()
		if err != nil {
			t.Fatalf("%s command failed bash -n: %v\n%s", name, err, out)
		}
	}
}
