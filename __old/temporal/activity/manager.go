package activity

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/temporal"
	"github.com/ponyruntime/pony/service/temporal/client"
	"go.uber.org/zap"
)

type activityHandler struct {
	config  *api.ActivityDefinition
	client  *client.Client
	handler interface{}
}

// Manager creates and manages activity handlers
type Manager struct {
	mu       sync.RWMutex
	log      *zap.Logger
	executor runtime.Executor
	handlers map[registry.ID]*activityHandler
}

// NewActivityManager creates a new activity manager instance
func NewActivityManager(log *zap.Logger, executor runtime.Executor) *Manager {
	return &Manager{
		log:      log,
		executor: executor,
		handlers: make(map[registry.ID]*activityHandler),
	}
}

// GetHandler retrieves an existing activity handler
func (m *Manager) GetHandler(id registry.ID) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ah, exists := m.handlers[id]
	if !exists {
		return nil, fmt.Errorf("activity handler %s not found", id)
	}

	return ah.handler, nil
}

// AddHandler initializes a new activity handler
func (m *Manager) AddHandler(
	id registry.ID,
	cfg *api.ActivityDefinition,
	client *client.Client,
) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.handlers[id]; exists {
		return nil, fmt.Errorf("activity handler %s already exists", id)
	}

	// Create handler function with execution logic
	handler := func(ctx context.Context, args payload.Payloads) (payload.Payloads, error) {
		ctx = client.OnContext(ctx)
		m.log.Debug("executing function activity",
			zap.String("activity_id", string(id)),
			zap.String("function_target", string(cfg.Function)),
		)

		// Get the activity and return results
		results, err := m.executeActivity(ctx, id, args)
		if err != nil {
			m.log.Warn("function activity execution failed",
				zap.String("activity_id", string(id)),
				zap.Error(err),
			)
			return nil, err
		}

		return results, nil
	}

	m.handlers[id] = &activityHandler{
		config:  cfg,
		client:  client,
		handler: handler,
	}

	m.log.Info("initialized activity handler", zap.String("id", string(id)))
	return handler, nil
}

// Delete removes an activity handler
func (m *Manager) Delete(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.handlers[id]; !exists {
		return fmt.Errorf("activity handler %s not found", id)
	}

	delete(m.handlers, id)
	m.log.Info("deleted activity handler", zap.String("id", string(id)))
	return nil
}

// Has checks if an activity handler exists
func (m *Manager) Has(id registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.handlers[id]
	return exists
}

// GetConfig retrieves an activity configuration
func (m *Manager) GetConfig(id registry.ID) (*api.ActivityDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if ah, exists := m.handlers[id]; exists {
		return ah.config, true
	}
	return nil, false
}
