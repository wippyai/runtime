package client

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
)

// Manager handles Temporal client configuration and lifecycle
type Manager struct {
	log      *zap.Logger
	configs  map[registry.ID]*api.ClientConfig
	services map[registry.ID]*Client
}

// NewClientManager creates a new client manager instance
func NewClientManager(logger *zap.Logger) *Manager {
	return &Manager{
		log:      logger,
		configs:  make(map[registry.ID]*api.ClientConfig),
		services: make(map[registry.ID]*Client),
	}
}

// AddClient initializes a new client instance with the given configuration
func (m *Manager) AddClient(id registry.ID, cfg *api.ClientConfig, dc converter.DataConverter) (*Client, error) {
	// Check if client already exists
	if _, exists := m.services[id]; exists {
		return nil, fmt.Errorf("client %s already initialized", id)
	}

	if _, exists := m.configs[id]; exists {
		return nil, fmt.Errorf("client config %s already exists", id)
	}

	// Create new service
	service := NewClient(m.log, id, dc, cfg)
	m.services[id] = service

	m.log.Info("initialized client", zap.String("id", string(id)))
	return service, nil
}

// GetClient retrieves an existing client by ID
func (m *Manager) GetClient(id registry.ID) (*Client, error) {
	service, exists := m.services[id]
	if !exists {
		return nil, fmt.Errorf("client %s not initialized", id)
	}
	return service, nil
}

// Update updates an existing client configuration
// Note: This will not affect already created service instances
func (m *Manager) Update(id registry.ID, config *api.ClientConfig) error {
	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("client config %s not found", id)
	}

	m.configs[id] = config
	m.log.Info("updated client config", zap.String("id", string(id)))
	return nil
}

// Delete removes a client configuration and service if it exists
func (m *Manager) Delete(id registry.ID) error {
	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("client config %s not found", id)
	}

	delete(m.configs, id)
	delete(m.services, id) // Controller cleanup should be handled by supervisor
	m.log.Info("deleted client", zap.String("id", string(id)))
	return nil
}

// GetConfig retrieves a client config by ID
func (m *Manager) GetConfig(id registry.ID) (*api.ClientConfig, bool) {
	config, exists := m.configs[id]
	return config, exists
}

// Has checks if a client config exists
func (m *Manager) Has(id registry.ID) bool {
	_, exists := m.configs[id]
	return exists
}
