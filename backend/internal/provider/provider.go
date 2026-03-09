package provider

import "context"

type CreateServerRequest struct {
	Name       string
	ServerType string
	Region     string
	Image      string
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

type InfraProvider interface {
	CreateServer(ctx context.Context, req CreateServerRequest) (*Server, error)
	GetServer(ctx context.Context, providerServerID string) (*Server, error)
	StartServer(ctx context.Context, providerServerID string) error
	StopServer(ctx context.Context, providerServerID string) error
	DeleteServer(ctx context.Context, providerServerID string) error
	RestartServer(ctx context.Context, providerServerID string) error
}
