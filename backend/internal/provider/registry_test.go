package provider

import (
	"context"
	"testing"
)

// mockProvider is a minimal InfraProvider implementation for testing.
type mockProvider struct {
	name string
}

func (m *mockProvider) CreateServer(_ context.Context, _ CreateServerRequest) (*Server, error) {
	return nil, nil
}

func (m *mockProvider) GetServer(_ context.Context, _ string) (*Server, error) {
	return nil, nil
}

func (m *mockProvider) StartServer(_ context.Context, _ string) error {
	return nil
}

func (m *mockProvider) StopServer(_ context.Context, _ string) error {
	return nil
}

func (m *mockProvider) DeleteServer(_ context.Context, _ string) error {
	return nil
}

func (m *mockProvider) RestartServer(_ context.Context, _ string) error {
	return nil
}

func (m *mockProvider) ResetPassword(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockProvider) CreateSnapshot(_ context.Context, _ string, _ string) (*SnapshotResult, error) {
	return nil, nil
}

func (m *mockProvider) DeleteSnapshot(_ context.Context, _ string) error {
	return nil
}

func (m *mockProvider) RebuildServer(_ context.Context, _ string, _ string) (string, error) {
	return "", nil
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.providers) != 0 {
		t.Fatalf("expected empty registry, got %d providers", len(r.providers))
	}
}

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	mock := &mockProvider{name: "hetzner"}

	r.Register("hetzner", mock)

	got, err := r.Get("hetzner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != mock {
		t.Fatal("Get returned a different provider than the one registered")
	}
}

func TestGetUnregistered(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for unregistered provider, got nil")
	}
}

func TestRegisterOverwrite(t *testing.T) {
	r := NewRegistry()
	first := &mockProvider{name: "first"}
	second := &mockProvider{name: "second"}

	r.Register("provider", first)
	r.Register("provider", second)

	got, err := r.Get("provider")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mp, ok := got.(*mockProvider)
	if !ok {
		t.Fatal("returned provider is not a *mockProvider")
	}
	if mp.name != "second" {
		t.Fatalf("expected overwritten provider 'second', got %q", mp.name)
	}
}

func TestMultipleProviders(t *testing.T) {
	r := NewRegistry()
	hetzner := &mockProvider{name: "hetzner"}
	aws := &mockProvider{name: "aws"}
	gcp := &mockProvider{name: "gcp"}

	r.Register("hetzner", hetzner)
	r.Register("aws", aws)
	r.Register("gcp", gcp)

	tests := []struct {
		name     string
		expected *mockProvider
	}{
		{"hetzner", hetzner},
		{"aws", aws},
		{"gcp", gcp},
	}

	for _, tc := range tests {
		got, err := r.Get(tc.name)
		if err != nil {
			t.Errorf("Get(%q): unexpected error: %v", tc.name, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("Get(%q): returned wrong provider", tc.name)
		}
	}
}
