package provider

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
)

type mockServerState struct {
	server *Server
	status string // "initializing", "running", "off", "stopping", "starting", "deleted"
	mu     sync.RWMutex
}

// MockConfig holds configurable delays for the mock provider.
type MockConfig struct {
	InitDelay      time.Duration
	HeartbeatDelay time.Duration
	StopDelay      time.Duration
	StartDelay     time.Duration
	RestartDelay   time.Duration
}

// DefaultMockConfig returns sensible defaults for dev testing.
func DefaultMockConfig() MockConfig {
	return MockConfig{
		InitDelay:      12 * time.Second,
		HeartbeatDelay: 18 * time.Second,
		StopDelay:      3 * time.Second,
		StartDelay:     5 * time.Second,
		RestartDelay:   8 * time.Second,
	}
}

// MockProvider is a stateful development provider that simulates realistic
// Hetzner behavior with timed state transitions and heartbeat simulation.
type MockProvider struct {
	pool    *pgxpool.Pool
	logger  *slog.Logger
	config  MockConfig
	servers map[string]*mockServerState
	mu      sync.RWMutex
}

func NewMockProvider(pool *pgxpool.Pool, logger *slog.Logger, cfg MockConfig) *MockProvider {
	return &MockProvider{
		pool:    pool,
		logger:  logger,
		config:  cfg,
		servers: make(map[string]*mockServerState),
	}
}

func (m *MockProvider) CreateServer(ctx context.Context, req CreateServerRequest) (*Server, error) {
	serverID := fmt.Sprintf("mock-%d", rand.IntN(999999))
	ip := fmt.Sprintf("10.0.%d.%d", rand.IntN(255), rand.IntN(255)+1)

	srv := &Server{
		ProviderServerID: serverID,
		Name:             req.Name,
		Status:           "initializing",
		IPv4:             ip,
		RootPassword:     "mock-password-12345",
	}

	state := &mockServerState{
		server: srv,
		status: "initializing",
	}

	m.mu.Lock()
	m.servers[serverID] = state
	m.mu.Unlock()

	m.logger.Info("mock: created server",
		"server_id", serverID,
		"name", req.Name,
		"region", req.Region,
		"ip", ip,
	)

	// Extract instance_id from labels for heartbeat simulation
	instanceIDStr := req.Labels["instance_id"]

	// Goroutine: transition initializing → running after InitDelay
	go func() {
		time.Sleep(m.config.InitDelay)

		state.mu.Lock()
		if state.status == "initializing" {
			state.status = "running"
			state.server.Status = "running"
			m.logger.Info("mock: server now running", "server_id", serverID)
		}
		state.mu.Unlock()

		// Goroutine: simulate heartbeat after HeartbeatDelay
		if instanceIDStr != "" {
			go func() {
				time.Sleep(m.config.HeartbeatDelay)

				state.mu.RLock()
				currentStatus := state.status
				state.mu.RUnlock()

				if currentStatus != "running" {
					m.logger.Info("mock: skipping heartbeat, server not running",
						"server_id", serverID, "status", currentStatus)
					return
				}

				instanceID, err := uuid.Parse(instanceIDStr)
				if err != nil {
					m.logger.Error("mock: invalid instance_id label",
						"instance_id", instanceIDStr, "error", err)
					return
				}

				if err := db.UpdateInstanceHeartbeat(context.Background(), m.pool, instanceID); err != nil {
					m.logger.Error("mock: heartbeat write failed",
						"instance_id", instanceIDStr, "error", err)
					return
				}

				m.logger.Info("mock: heartbeat sent",
					"server_id", serverID, "instance_id", instanceIDStr)
			}()
		}
	}()

	return srv, nil
}

func (m *MockProvider) GetServer(ctx context.Context, providerServerID string) (*Server, error) {
	m.mu.RLock()
	state, ok := m.servers[providerServerID]
	m.mu.RUnlock()

	if !ok {
		// Unknown server — might be from a previous process restart.
		// Return not found so the reconciler can handle it.
		return nil, fmt.Errorf("mock: server %s not found", providerServerID)
	}

	state.mu.RLock()
	defer state.mu.RUnlock()

	if state.status == "deleted" {
		return nil, fmt.Errorf("mock: server %s not found", providerServerID)
	}

	return &Server{
		ProviderServerID: state.server.ProviderServerID,
		Name:             state.server.Name,
		Status:           state.status,
		IPv4:             state.server.IPv4,
	}, nil
}

func (m *MockProvider) StartServer(ctx context.Context, providerServerID string) error {
	state, err := m.getState(providerServerID)
	if err != nil {
		return err
	}

	state.mu.Lock()
	if state.status != "off" {
		state.mu.Unlock()
		return fmt.Errorf("mock: cannot start server in state %s", state.status)
	}
	state.status = "starting"
	state.server.Status = "starting"
	state.mu.Unlock()

	m.logger.Info("mock: starting server", "server_id", providerServerID)

	go func() {
		time.Sleep(m.config.StartDelay)
		state.mu.Lock()
		if state.status == "starting" {
			state.status = "running"
			state.server.Status = "running"
			m.logger.Info("mock: server now running", "server_id", providerServerID)
		}
		state.mu.Unlock()
	}()

	return nil
}

func (m *MockProvider) StopServer(ctx context.Context, providerServerID string) error {
	state, err := m.getState(providerServerID)
	if err != nil {
		return err
	}

	state.mu.Lock()
	if state.status != "running" {
		state.mu.Unlock()
		return fmt.Errorf("mock: cannot stop server in state %s", state.status)
	}
	state.status = "stopping"
	state.server.Status = "stopping"
	state.mu.Unlock()

	m.logger.Info("mock: stopping server", "server_id", providerServerID)

	go func() {
		time.Sleep(m.config.StopDelay)
		state.mu.Lock()
		if state.status == "stopping" {
			state.status = "off"
			state.server.Status = "off"
			m.logger.Info("mock: server now off", "server_id", providerServerID)
		}
		state.mu.Unlock()
	}()

	return nil
}

func (m *MockProvider) DeleteServer(ctx context.Context, providerServerID string) error {
	state, err := m.getState(providerServerID)
	if err != nil {
		// Already gone — idempotent delete
		m.logger.Info("mock: delete server (already gone)", "server_id", providerServerID)
		return nil
	}

	state.mu.Lock()
	state.status = "deleted"
	state.server.Status = "deleted"
	state.mu.Unlock()

	m.logger.Info("mock: deleted server", "server_id", providerServerID)
	return nil
}

func (m *MockProvider) RestartServer(ctx context.Context, providerServerID string) error {
	state, err := m.getState(providerServerID)
	if err != nil {
		return err
	}

	state.mu.Lock()
	state.status = "stopping"
	state.server.Status = "stopping"
	state.mu.Unlock()

	m.logger.Info("mock: restarting server", "server_id", providerServerID)

	go func() {
		// Stop phase
		time.Sleep(m.config.RestartDelay / 2)
		state.mu.Lock()
		if state.status == "stopping" {
			state.status = "starting"
			state.server.Status = "starting"
		}
		state.mu.Unlock()

		// Start phase
		time.Sleep(m.config.RestartDelay / 2)
		state.mu.Lock()
		if state.status == "starting" {
			state.status = "running"
			state.server.Status = "running"
			m.logger.Info("mock: server restarted", "server_id", providerServerID)
		}
		state.mu.Unlock()
	}()

	return nil
}

func (m *MockProvider) ResetPassword(ctx context.Context, providerServerID string) (string, error) {
	_, err := m.getState(providerServerID)
	if err != nil {
		return "", err
	}
	newPassword := fmt.Sprintf("mock-reset-%d", rand.IntN(99999))
	m.logger.Info("mock: password reset", "server_id", providerServerID)
	return newPassword, nil
}

func (m *MockProvider) getState(providerServerID string) (*mockServerState, error) {
	m.mu.RLock()
	state, ok := m.servers[providerServerID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("mock: server %s not found", providerServerID)
	}

	state.mu.RLock()
	if state.status == "deleted" {
		state.mu.RUnlock()
		return nil, fmt.Errorf("mock: server %s not found", providerServerID)
	}
	state.mu.RUnlock()

	return state, nil
}
