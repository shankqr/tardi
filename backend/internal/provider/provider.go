package provider

import (
	"context"
	"errors"
)

// ErrServerNotFound is returned when a server no longer exists at the provider.
var ErrServerNotFound = errors.New("server not found")

type CreateServerRequest struct {
	Name       string
	ServerType string
	Region     string
	Image      string // OS image name (e.g., "ubuntu-24.04")
	ImageID    string // Provider snapshot/image ID — takes precedence over Image if set
	UserData   string
	Labels     map[string]string
}

type Server struct {
	ProviderServerID string
	Name             string
	Status           string // "initializing", "running", "off"
	IPv4             string
	RootPassword     string // Only set on initial creation
}

type SnapshotResult struct {
	ProviderImageID string
	SizeGB          float32
}

type InfraProvider interface {
	CreateServer(ctx context.Context, req CreateServerRequest) (*Server, error)
	GetServer(ctx context.Context, providerServerID string) (*Server, error)
	StartServer(ctx context.Context, providerServerID string) error
	StopServer(ctx context.Context, providerServerID string) error
	DeleteServer(ctx context.Context, providerServerID string) error
	RestartServer(ctx context.Context, providerServerID string) error
	ResetPassword(ctx context.Context, providerServerID string) (string, error)
	CreateSnapshot(ctx context.Context, providerServerID string, description string) (*SnapshotResult, error)
	DeleteSnapshot(ctx context.Context, providerImageID string) error
	RebuildServer(ctx context.Context, providerServerID string, providerImageID string) (string, error)
}
