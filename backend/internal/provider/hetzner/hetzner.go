package hetzner

import (
	"context"
	"errors"
	"log/slog"

	"github.com/shanq/tardi/internal/provider"
)

// HetznerProvider implements the InfraProvider interface using the Hetzner Cloud API.
// This is a skeleton — real implementation will use hcloud-go/v2.
type HetznerProvider struct {
	apiToken string
	logger   *slog.Logger
}

func New(apiToken string, logger *slog.Logger) *HetznerProvider {
	return &HetznerProvider{
		apiToken: apiToken,
		logger:   logger,
	}
}

var errNotImplemented = errors.New("hetzner provider: not yet implemented")

func (h *HetznerProvider) CreateServer(ctx context.Context, req provider.CreateServerRequest) (*provider.Server, error) {
	return nil, errNotImplemented
}

func (h *HetznerProvider) GetServer(ctx context.Context, providerServerID string) (*provider.Server, error) {
	return nil, errNotImplemented
}

func (h *HetznerProvider) StartServer(ctx context.Context, providerServerID string) error {
	return errNotImplemented
}

func (h *HetznerProvider) StopServer(ctx context.Context, providerServerID string) error {
	return errNotImplemented
}

func (h *HetznerProvider) DeleteServer(ctx context.Context, providerServerID string) error {
	return errNotImplemented
}

func (h *HetznerProvider) RestartServer(ctx context.Context, providerServerID string) error {
	return errNotImplemented
}
