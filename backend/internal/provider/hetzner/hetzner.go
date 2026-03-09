package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/shanq/tardi/internal/provider"
)

// HetznerProvider implements the InfraProvider interface using the Hetzner Cloud API.
type HetznerProvider struct {
	client *hcloud.Client
	logger *slog.Logger
}

func New(apiToken string, logger *slog.Logger) *HetznerProvider {
	client := hcloud.NewClient(hcloud.WithToken(apiToken))
	return &HetznerProvider{
		client: client,
		logger: logger,
	}
}

func (h *HetznerProvider) CreateServer(ctx context.Context, req provider.CreateServerRequest) (*provider.Server, error) {
	serverType := &hcloud.ServerType{Name: req.ServerType}
	image := &hcloud.Image{Name: req.Image}
	location := &hcloud.Location{Name: req.Region}

	labels := req.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	opts := hcloud.ServerCreateOpts{
		Name:       req.Name,
		ServerType: serverType,
		Image:      image,
		Location:   location,
		Labels:     labels,
		UserData:   req.UserData,
	}

	result, _, err := h.client.Server.Create(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("hetzner create server: %w", err)
	}

	server := result.Server
	h.logger.Info("hetzner: server created",
		"server_id", server.ID,
		"name", server.Name,
		"status", server.Status,
	)

	ipv4 := ""
	if server.PublicNet.IPv4.IP != nil {
		ipv4 = server.PublicNet.IPv4.IP.String()
	}

	return &provider.Server{
		ProviderServerID: strconv.FormatInt(server.ID, 10),
		Name:             server.Name,
		Status:           string(server.Status),
		IPv4:             ipv4,
		RootPassword:     result.RootPassword,
	}, nil
}

func (h *HetznerProvider) GetServer(ctx context.Context, providerServerID string) (*provider.Server, error) {
	id, err := strconv.ParseInt(providerServerID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid server id: %w", err)
	}

	server, _, err := h.client.Server.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("hetzner get server: %w", err)
	}
	if server == nil {
		return nil, fmt.Errorf("hetzner server %s not found", providerServerID)
	}

	ipv4 := ""
	if server.PublicNet.IPv4.IP != nil {
		ipv4 = server.PublicNet.IPv4.IP.String()
	}

	return &provider.Server{
		ProviderServerID: providerServerID,
		Name:             server.Name,
		Status:           string(server.Status),
		IPv4:             ipv4,
	}, nil
}

func (h *HetznerProvider) StartServer(ctx context.Context, providerServerID string) error {
	id, err := strconv.ParseInt(providerServerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid server id: %w", err)
	}

	server := &hcloud.Server{ID: id}
	_, _, err = h.client.Server.Poweron(ctx, server)
	if err != nil {
		return fmt.Errorf("hetzner start server: %w", err)
	}

	h.logger.Info("hetzner: server started", "server_id", providerServerID)
	return nil
}

func (h *HetznerProvider) StopServer(ctx context.Context, providerServerID string) error {
	id, err := strconv.ParseInt(providerServerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid server id: %w", err)
	}

	server := &hcloud.Server{ID: id}
	_, _, err = h.client.Server.Shutdown(ctx, server)
	if err != nil {
		return fmt.Errorf("hetzner stop server: %w", err)
	}

	h.logger.Info("hetzner: server stopped", "server_id", providerServerID)
	return nil
}

func (h *HetznerProvider) DeleteServer(ctx context.Context, providerServerID string) error {
	id, err := strconv.ParseInt(providerServerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid server id: %w", err)
	}

	server := &hcloud.Server{ID: id}
	_, _, err = h.client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return fmt.Errorf("hetzner delete server: %w", err)
	}

	h.logger.Info("hetzner: server deleted", "server_id", providerServerID)
	return nil
}

func (h *HetznerProvider) ResetPassword(ctx context.Context, providerServerID string) (string, error) {
	id, err := strconv.ParseInt(providerServerID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid server id: %w", err)
	}

	server := &hcloud.Server{ID: id}
	result, _, err := h.client.Server.ResetPassword(ctx, server)
	if err != nil {
		return "", fmt.Errorf("hetzner reset password: %w", err)
	}

	h.logger.Info("hetzner: password reset", "server_id", providerServerID)
	return result.RootPassword, nil
}

func (h *HetznerProvider) RestartServer(ctx context.Context, providerServerID string) error {
	id, err := strconv.ParseInt(providerServerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid server id: %w", err)
	}

	server := &hcloud.Server{ID: id}
	_, _, err = h.client.Server.Reboot(ctx, server)
	if err != nil {
		return fmt.Errorf("hetzner restart server: %w", err)
	}

	h.logger.Info("hetzner: server restarted", "server_id", providerServerID)
	return nil
}
