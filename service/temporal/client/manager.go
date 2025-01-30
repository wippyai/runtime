package client

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/temporal"
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

// Add adds a new client configuration
func (m *Manager) Add(id registry.ID, config *api.ClientConfig) error {
	if _, exists := m.configs[id]; exists {
		return fmt.Errorf("client %s already exists", id)
	}

	m.configs[id] = config
	m.log.Info("added client config", zap.String("id", string(id)))
	return nil
}

// Update updates an existing client configuration
// Note: This will not affect already created service instances
func (m *Manager) Update(id registry.ID, config *api.ClientConfig) error {
	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("client %s not found", id)
	}

	m.configs[id] = config
	m.log.Info("updated client config", zap.String("id", string(id)))
	return nil
}

// Delete removes a client configuration and service if it exists
func (m *Manager) Delete(id registry.ID) error {
	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("client %s not found", id)
	}

	delete(m.configs, id)
	delete(m.services, id) // Service cleanup should be handled by supervisor
	m.log.Info("deleted client", zap.String("id", string(id)))
	return nil
}

// GetConfig retrieves a client config by ID
func (m *Manager) GetConfig(id registry.ID) (*api.ClientConfig, bool) {
	config, exists := m.configs[id]
	return config, exists
}

// GetClient returns an existing service or creates a new one
func (m *Manager) GetClient(id registry.ID) (*Client, error) {
	// Check for existing service
	if service, exists := m.services[id]; exists {
		return service, nil
	}

	// Get config
	config, exists := m.configs[id]
	if !exists {
		return nil, fmt.Errorf("client %s not found", id)
	}

	// Create new service
	service := NewClient(m.log, config)
	m.services[id] = service

	return service, nil
}

// Has checks if a client exists
func (m *Manager) Has(id registry.ID) bool {
	_, exists := m.configs[id]
	return exists
}
