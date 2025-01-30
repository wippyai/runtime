package activity

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/service/temporal/client"
	"go.uber.org/zap"
)

// Manager creates and manages activity handlers
type Manager struct {
	mu       sync.RWMutex
	log      *zap.Logger
	executor runtime.Executor
	configs  map[registry.ID]*api.FunctionActivity
}

// NewActivityManager creates a new activity manager instance
func NewActivityManager(log *zap.Logger, executor runtime.Executor) *Manager {
	return &Manager{
		log:      log,
		executor: executor,
		configs:  make(map[registry.ID]*api.FunctionActivity),
	}
}

// executeActivity handles the actual execution of an activity through the runtime executor
func (m *Manager) executeActivity(ctx context.Context, id registry.ID, inputs payload.Payloads) (payload.Payloads, error) {
	cfg, exists := m.Get(id)
	if !exists {
		return nil, fmt.Errorf("activity configuration %s not found", id)
	}

	// Create a task for the executor
	task := runtime.Task{
		Context:  ctx,
		Target:   registry.ID(cfg.Function), // Use the function target from config
		Payloads: inputs,                    // Pass through the input payloads
	}

	// Execute the task
	resultCh, err := m.executor.Execute(task)
	if err != nil {
		return nil, fmt.Errorf("failed to execute activity task: %w", err)
	}

	// Wait for result with context cancellation handling
	select {
	case result := <-resultCh:
		if result == nil {
			return nil, fmt.Errorf("received nil result from executor")
		}
		if result.Error != nil {
			return nil, fmt.Errorf("activity execution failed: %w", result.Error)
		}

		// Convert single payload to payloads slice if needed
		if result.Payload != nil {
			return payload.Payloads{result.Payload}, nil
		}

		return payload.Payloads{}, nil

	case <-ctx.Done():
		return nil, fmt.Errorf("activity execution cancelled: %w", ctx.Err())
	}
}

// Register creates an activity handler function
// The client is used only for context binding in the returned handler
func (m *Manager) Register(
	id registry.ID,
	cfg *api.FunctionActivity,
	client *client.Client,
) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store the config
	m.configs[id] = cfg

	// Create handler function with execution logic
	handler := func(ctx context.Context, args payload.Payloads) (payload.Payloads, error) {
		ctx = client.OnContext(ctx)
		m.log.Debug("executing activity",
			zap.String("activity_id", string(id)),
			zap.String("function_target", string(cfg.Function)),
		)

		// Execute the activity and return results
		results, err := m.executeActivity(ctx, id, args)
		if err != nil {
			m.log.Warn("activity execution failed",
				zap.String("activity_id", string(id)),
				zap.Error(err),
			)
			return nil, err
		}

		return results, nil
	}

	m.log.Info("registered activity handler", zap.String("id", string(id)))
	return handler, nil
}

// Get retrieves an activity configuration
func (m *Manager) Get(id registry.ID) (*api.FunctionActivity, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, exists := m.configs[id]
	return cfg, exists
}

// Delete removes an activity configuration
func (m *Manager) Delete(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("activity %s not found", id)
	}

	delete(m.configs, id)
	m.log.Info("deleted activity configuration", zap.String("id", string(id)))
	return nil
}

// Has checks if an activity configuration exists
func (m *Manager) Has(id registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.configs[id]
	return exists
}
