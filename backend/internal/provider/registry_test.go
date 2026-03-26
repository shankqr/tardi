package provider

import (
	"context"
	"log/slog"
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	stub := NewStubProvider(slog.Default())

	reg.Register("stub", stub)

	got, err := reg.Get("stub")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != stub {
		t.Errorf("Get returned different provider instance")
	}
}

func TestRegistry_GetUnregistered(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Fatal("Get should return error for unregistered provider")
	}
	if got := err.Error(); got != "provider not registered: nonexistent" {
		t.Errorf("error = %q, want %q", got, "provider not registered: nonexistent")
	}
}

func TestRegistry_MultipleProviders(t *testing.T) {
	reg := NewRegistry()
	stub1 := NewStubProvider(slog.Default())
	stub2 := NewStubProvider(slog.Default())

	reg.Register("provider-a", stub1)
	reg.Register("provider-b", stub2)

	gotA, err := reg.Get("provider-a")
	if err != nil {
		t.Fatalf("Get provider-a: %v", err)
	}
	if gotA != stub1 {
		t.Error("provider-a returned wrong instance")
	}

	gotB, err := reg.Get("provider-b")
	if err != nil {
		t.Fatalf("Get provider-b: %v", err)
	}
	if gotB != stub2 {
		t.Error("provider-b returned wrong instance")
	}
}

func TestRegistry_OverwriteProvider(t *testing.T) {
	reg := NewRegistry()
	stub1 := NewStubProvider(slog.Default())
	stub2 := NewStubProvider(slog.Default())

	reg.Register("same-name", stub1)
	reg.Register("same-name", stub2)

	got, err := reg.Get("same-name")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != stub2 {
		t.Error("expected second registration to overwrite first")
	}
}

func TestStubProvider_ImplementsInterface(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	var _ InfraProvider = stub // compile-time check
}

func TestStubProvider_CreateServer(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	ctx := context.Background()

	srv, err := stub.CreateServer(ctx, CreateServerRequest{
		Name:       "test-server",
		ServerType: "cx11",
		Region:     "fsn1",
		Image:      "ubuntu-24.04",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if srv.Name != "test-server" {
		t.Errorf("Name = %q, want %q", srv.Name, "test-server")
	}
	if srv.Status != "initializing" {
		t.Errorf("Status = %q, want %q", srv.Status, "initializing")
	}
	if srv.IPv4 == "" {
		t.Error("IPv4 should not be empty")
	}
	if srv.ProviderServerID == "" {
		t.Error("ProviderServerID should not be empty")
	}
}

func TestStubProvider_GetServer(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	ctx := context.Background()

	srv, err := stub.GetServer(ctx, "stub-123")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if srv.ProviderServerID != "stub-123" {
		t.Errorf("ProviderServerID = %q, want %q", srv.ProviderServerID, "stub-123")
	}
	if srv.Status != "running" {
		t.Errorf("Status = %q, want %q", srv.Status, "running")
	}
}

func TestStubProvider_StartStopDeleteServer(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	ctx := context.Background()

	if err := stub.StartServer(ctx, "stub-1"); err != nil {
		t.Errorf("StartServer: %v", err)
	}
	if err := stub.StopServer(ctx, "stub-1"); err != nil {
		t.Errorf("StopServer: %v", err)
	}
	if err := stub.DeleteServer(ctx, "stub-1"); err != nil {
		t.Errorf("DeleteServer: %v", err)
	}
}

func TestStubProvider_RestartServer(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	ctx := context.Background()

	if err := stub.RestartServer(ctx, "stub-1"); err != nil {
		t.Errorf("RestartServer: %v", err)
	}
}

func TestStubProvider_ResetPassword(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	ctx := context.Background()

	pw, err := stub.ResetPassword(ctx, "stub-1")
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if pw != "stub-password-12345" {
		t.Errorf("password = %q, want %q", pw, "stub-password-12345")
	}
}

func TestStubProvider_CreateSnapshot(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	ctx := context.Background()

	snap, err := stub.CreateSnapshot(ctx, "stub-1", "test snapshot")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if snap.ProviderImageID != "stub-snap-1" {
		t.Errorf("ProviderImageID = %q, want %q", snap.ProviderImageID, "stub-snap-1")
	}
	if snap.SizeGB != 1.0 {
		t.Errorf("SizeGB = %f, want 1.0", snap.SizeGB)
	}
}

func TestStubProvider_DeleteSnapshot(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	ctx := context.Background()

	if err := stub.DeleteSnapshot(ctx, "stub-snap-1"); err != nil {
		t.Errorf("DeleteSnapshot: %v", err)
	}
}

func TestStubProvider_RebuildServer(t *testing.T) {
	stub := NewStubProvider(slog.Default())
	ctx := context.Background()

	pw, err := stub.RebuildServer(ctx, "stub-1", "stub-snap-1")
	if err != nil {
		t.Fatalf("RebuildServer: %v", err)
	}
	if pw != "stub-password-12345" {
		t.Errorf("password = %q, want %q", pw, "stub-password-12345")
	}
}
