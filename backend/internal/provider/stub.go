package provider

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
)

// StubProvider is a development provider that simulates infrastructure operations.
type StubProvider struct {
	logger *slog.Logger
}

func NewStubProvider(logger *slog.Logger) *StubProvider {
	return &StubProvider{logger: logger}
}

func (s *StubProvider) CreateServer(ctx context.Context, req CreateServerRequest) (*Server, error) {
	serverID := fmt.Sprintf("stub-%d", rand.IntN(999999))
	ip := fmt.Sprintf("10.0.%d.%d", rand.IntN(255), rand.IntN(255)+1)

	s.logger.Info("stub: created server",
		"server_id", serverID,
		"name", req.Name,
		"type", req.ServerType,
		"region", req.Region,
		"ip", ip,
	)

	return &Server{
		ProviderServerID: serverID,
		Name:             req.Name,
		Status:           "initializing",
		IPv4:             ip,
	}, nil
}

func (s *StubProvider) GetServer(ctx context.Context, providerServerID string) (*Server, error) {
	s.logger.Info("stub: get server", "server_id", providerServerID)
	return &Server{
		ProviderServerID: providerServerID,
		Status:           "running",
	}, nil
}

func (s *StubProvider) StartServer(ctx context.Context, providerServerID string) error {
	s.logger.Info("stub: start server", "server_id", providerServerID)
	return nil
}

func (s *StubProvider) StopServer(ctx context.Context, providerServerID string) error {
	s.logger.Info("stub: stop server", "server_id", providerServerID)
	return nil
}

func (s *StubProvider) DeleteServer(ctx context.Context, providerServerID string) error {
	s.logger.Info("stub: delete server", "server_id", providerServerID)
	return nil
}

func (s *StubProvider) RestartServer(ctx context.Context, providerServerID string) error {
	s.logger.Info("stub: restart server", "server_id", providerServerID)
	return nil
}
